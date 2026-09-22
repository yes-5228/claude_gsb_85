package overdue_test

import (
	"context"
	"testing"
	"time"

	"github.com/drainage/desilting/internal/httpx"
	"github.com/drainage/desilting/internal/modules/cleaningtask"
	"github.com/drainage/desilting/internal/modules/dashboard"
	"github.com/drainage/desilting/internal/modules/overdue"
	"github.com/drainage/desilting/internal/shared/date"
	"github.com/drainage/desilting/internal/testsupport"
)

// createTaskWithPlan 按指定计划日期创建待开工任务。
func createTaskWithPlan(t *testing.T, fixture *testsupport.Fixture, title string, planStart, planEnd date.Date) *cleaningtask.CleaningTask {
	t.Helper()
	task, err := fixture.Tasks.Create(context.Background(), cleaningtask.SaveRequest{
		Title:         title,
		PipeSegmentID: fixture.Segment.ID,
		Priority:      cleaningtask.PriorityNormal,
		Source:        cleaningtask.SourcePlan,
		PlanStartDate: planStart,
		PlanEndDate:   planEnd,
		TeamName:      "测试班组",
	})
	testsupport.RequireNoError(t, err)
	return task
}

// activeWarnings 查询当前待处理预警。
func activeWarnings(t *testing.T, fixture *testsupport.Fixture) []overdue.WarningItem {
	t.Helper()
	items, _, err := fixture.Overdue.List(context.Background(), overdue.WarningListQuery{
		Status: overdue.WarningActive,
		Page:   httpx.PageQuery{Page: 1, PageSize: 100},
	})
	testsupport.RequireNoError(t, err)
	return items
}

// warningsOf 取出某任务的待处理预警。
func warningsOf(items []overdue.WarningItem, taskID uint) []overdue.WarningItem {
	out := make([]overdue.WarningItem, 0)
	for _, item := range items {
		if item.TaskID == taskID {
			out = append(out, item)
		}
	}
	return out
}

func TestPendingTaskLateStartGetsNoticeWarning(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	today := date.Today()
	// 计划开始 5 天前（超过默认 3 天阈值），计划完成日在未来。
	task := createTaskWithPlan(t, fixture, "迟迟未开工的任务", today.AddDays(-5), today.AddDays(2))

	items := activeWarnings(t, fixture)
	found := warningsOf(items, task.ID)
	if len(found) != 1 {
		t.Fatalf("期望生成 1 条预警，实际 %d 条", len(found))
	}
	if found[0].Stage != overdue.StageStart {
		t.Fatalf("期望未按期开工预警，实际阶段 %s", found[0].Stage)
	}
	if found[0].Level != overdue.LevelNotice {
		t.Fatalf("未按期开工默认应为提醒级别，实际 %s", found[0].Level)
	}
	if found[0].OverdueDays != 2 {
		t.Fatalf("超期天数应为 2 天，实际 %d", found[0].OverdueDays)
	}
}

func TestPendingTaskFallsIntoSingleStageOnly(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	today := date.Today()
	// 计划开始与计划完成都已过：开工与报验阈值同时越过，只能落入更靠后的阶段。
	task := createTaskWithPlan(t, fixture, "开工报验都超期的任务", today.AddDays(-10), today.AddDays(-4))

	found := warningsOf(activeWarnings(t, fixture), task.ID)
	if len(found) != 1 {
		t.Fatalf("同一任务不能同时落入两个阶段，实际 %d 条预警", len(found))
	}
	if found[0].Stage != overdue.StageFinish {
		t.Fatalf("应落入更靠后的未按期报验阶段，实际 %s", found[0].Stage)
	}
	if found[0].Level != overdue.LevelWarning {
		t.Fatalf("未按期报验默认应为警告级别，实际 %s", found[0].Level)
	}
}

func TestInProgressTaskLateReportGetsWarning(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	today := date.Today()
	task := createTaskWithPlan(t, fixture, "施工中但未报验的任务", today.AddDays(-9), today.AddDays(-3))
	_, err := fixture.Tasks.Start(context.Background(), task.ID)
	testsupport.RequireNoError(t, err)

	found := warningsOf(activeWarnings(t, fixture), task.ID)
	if len(found) != 1 || found[0].Stage != overdue.StageFinish {
		t.Fatalf("清淤中任务计划完成已过应落入未按期报验，实际 %+v", found)
	}
}

func TestCompletedTaskLongPendingAcceptanceGetsCriticalWarning(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	today := date.Today()
	task := createTaskWithPlan(t, fixture, "报验后久未验收的任务", today.AddDays(-20), today.AddDays(-15))
	fixture.CreateRecord(t, task.ID, 10)
	_, err := fixture.Tasks.Complete(context.Background(), task.ID)
	testsupport.RequireNoError(t, err)
	// 把完工报验时间回拨到 10 天前，越过默认 7 天的验收超期阈值。
	fixture.DB.Exec("UPDATE cleaning_tasks SET finished_at = ? WHERE id = ?", time.Now().AddDate(0, 0, -10), task.ID)

	found := warningsOf(activeWarnings(t, fixture), task.ID)
	if len(found) != 1 || found[0].Stage != overdue.StageAccept {
		t.Fatalf("报验后长期未验收应落入验收超期，实际 %+v", found)
	}
	if found[0].Level != overdue.LevelCritical {
		t.Fatalf("验收超期默认应为严重级别，实际 %s", found[0].Level)
	}
	if found[0].OverdueDays != 3 {
		t.Fatalf("验收超期天数应为 3 天，实际 %d", found[0].OverdueDays)
	}
}

func TestCancelledAndAcceptedTasksAreExcluded(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	today := date.Today()

	cancelled := createTaskWithPlan(t, fixture, "已取消的超期任务", today.AddDays(-10), today.AddDays(-5))
	_, err := fixture.Tasks.Cancel(context.Background(), cancelled.ID, "计划调整")
	testsupport.RequireNoError(t, err)

	accepted := createTaskWithPlan(t, fixture, "已验收的任务", today.AddDays(-20), today.AddDays(-15))
	fixture.CreateRecord(t, accepted.ID, 10)
	_, err = fixture.Tasks.Complete(context.Background(), accepted.ID)
	testsupport.RequireNoError(t, err)
	fixture.DB.Exec("UPDATE cleaning_tasks SET finished_at = ? WHERE id = ?", time.Now().AddDate(0, 0, -10), accepted.ID)
	_, err = fixture.Acceptances.Create(context.Background(), testsupport.PassRequest(accepted.ID, 90))
	testsupport.RequireNoError(t, err)

	items := activeWarnings(t, fixture)
	if got := warningsOf(items, cancelled.ID); len(got) != 0 {
		t.Fatalf("已取消任务不参与超期统计，实际 %+v", got)
	}
	if got := warningsOf(items, accepted.ID); len(got) != 0 {
		t.Fatalf("已验收任务不参与超期统计，实际 %+v", got)
	}
}

func TestRepeatedSyncDoesNotRemindAgain(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	today := date.Today()
	task := createTaskWithPlan(t, fixture, "不重复提醒的任务", today.AddDays(-6), today.AddDays(2))

	first := warningsOf(activeWarnings(t, fixture), task.ID)
	// 连续多次同步，预警记录不能重复生成。
	for i := 0; i < 3; i++ {
		testsupport.RequireNoError(t, fixture.Overdue.Sync(context.Background()))
	}
	second := warningsOf(activeWarnings(t, fixture), task.ID)

	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("同一任务同一阶段只能提醒一次，首次 %d 条，再次 %d 条", len(first), len(second))
	}
	if first[0].ID != second[0].ID {
		t.Fatalf("反复同步不应生成新预警，首次 ID %d，再次 ID %d", first[0].ID, second[0].ID)
	}

	// 含已解除在内的全部记录也只能有一条。
	var total int64
	fixture.DB.Model(&overdue.OverdueWarning{}).Where("task_id = ? AND stage = ?", task.ID, overdue.StageStart).Count(&total)
	if total != 1 {
		t.Fatalf("预警记录总数应为 1，实际 %d", total)
	}
}

func TestWarningResolvedWhenTaskProgresses(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	today := date.Today()
	task := createTaskWithPlan(t, fixture, "开工后解除预警的任务", today.AddDays(-5), today.AddDays(2))
	if got := warningsOf(activeWarnings(t, fixture), task.ID); len(got) != 1 {
		t.Fatalf("前置条件：应有 1 条待处理预警，实际 %d", len(got))
	}

	_, err := fixture.Tasks.Start(context.Background(), task.ID)
	testsupport.RequireNoError(t, err)
	if got := warningsOf(activeWarnings(t, fixture), task.ID); len(got) != 0 {
		t.Fatalf("开工后未按期开工预警应解除，实际 %+v", got)
	}

	// 解除记录可追溯。
	resolved, _, err := fixture.Overdue.List(context.Background(), overdue.WarningListQuery{
		Status: overdue.WarningResolved,
		Page:   httpx.PageQuery{Page: 1, PageSize: 100},
	})
	testsupport.RequireNoError(t, err)
	found := warningsOf(resolved, task.ID)
	if len(found) != 1 || found[0].ResolveReason != "任务已开工" {
		t.Fatalf("解除原因应为任务已开工，实际 %+v", found)
	}
}

func TestWarningResolvedWhenTaskCancelled(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	today := date.Today()
	task := createTaskWithPlan(t, fixture, "取消后解除预警的任务", today.AddDays(-5), today.AddDays(2))
	if got := warningsOf(activeWarnings(t, fixture), task.ID); len(got) != 1 {
		t.Fatalf("前置条件：应有 1 条待处理预警，实际 %d", len(got))
	}

	_, err := fixture.Tasks.Cancel(context.Background(), task.ID, "汛期调度调整")
	testsupport.RequireNoError(t, err)
	if got := warningsOf(activeWarnings(t, fixture), task.ID); len(got) != 0 {
		t.Fatalf("取消后预警应解除，实际 %+v", got)
	}
}

func TestStageMigratesInsteadOfCoexisting(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	today := date.Today()
	task := createTaskWithPlan(t, fixture, "阶段迁移的任务", today.AddDays(-5), today.AddDays(2))
	if got := warningsOf(activeWarnings(t, fixture), task.ID); len(got) != 1 || got[0].Stage != overdue.StageStart {
		t.Fatalf("前置条件：应为未按期开工预警，实际 %+v", got)
	}

	// 计划完成日期也错过之后，预警应从「未按期开工」迁移为「未按期报验」。
	_, err := fixture.Tasks.Update(context.Background(), task.ID, cleaningtask.SaveRequest{
		Title:         "阶段迁移的任务",
		PipeSegmentID: fixture.Segment.ID,
		PlanStartDate: today.AddDays(-5),
		PlanEndDate:   today.AddDays(-1),
	})
	testsupport.RequireNoError(t, err)

	found := warningsOf(activeWarnings(t, fixture), task.ID)
	if len(found) != 1 || found[0].Stage != overdue.StageFinish {
		t.Fatalf("阶段应迁移为未按期报验且只有一条，实际 %+v", found)
	}
}

func TestFutureEffectiveRuleDoesNotApply(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	today := date.Today()

	// 明天才生效的更严格规则（阈值 0 天）不应影响今天的判定。
	_, err := fixture.Overdue.CreateRule(context.Background(), overdue.RuleSaveRequest{
		Stage:         overdue.StageStart,
		ThresholdDays: 0,
		Level:         overdue.LevelCritical,
		EffectiveFrom: today.AddDays(1),
	})
	testsupport.RequireNoError(t, err)

	// 计划开始 1 天前：按当前生效的 3 天阈值不超期，按未来规则则超期。
	task := createTaskWithPlan(t, fixture, "未来规则不生效的任务", today.AddDays(-1), today.AddDays(5))
	if got := warningsOf(activeWarnings(t, fixture), task.ID); len(got) != 0 {
		t.Fatalf("未来生效的规则不应参与当前判定，实际 %+v", got)
	}
}

func TestRuleChangeOnlyAffectsFutureWarnings(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	today := date.Today()

	// 任务一在旧规则（阈值 3 天 / 提醒）下生成预警。
	old := createTaskWithPlan(t, fixture, "旧规则下的任务", today.AddDays(-5), today.AddDays(5))
	first := warningsOf(activeWarnings(t, fixture), old.ID)
	if len(first) != 1 || first[0].Level != overdue.LevelNotice || first[0].ThresholdDays != 3 {
		t.Fatalf("前置条件：旧规则预警快照应为 提醒/3 天，实际 %+v", first)
	}

	// 调整规则：阈值 0 天 / 严重，今天生效。
	_, err := fixture.Overdue.CreateRule(context.Background(), overdue.RuleSaveRequest{
		Stage:         overdue.StageStart,
		ThresholdDays: 0,
		Level:         overdue.LevelCritical,
		EffectiveFrom: today,
	})
	testsupport.RequireNoError(t, err)

	// 任务二在新规则下判定（计划开始 1 天前即超期）。
	fresh := createTaskWithPlan(t, fixture, "新规则下的任务", today.AddDays(-1), today.AddDays(5))
	items := activeWarnings(t, fixture)

	kept := warningsOf(items, old.ID)
	if len(kept) != 1 || kept[0].ID != first[0].ID || kept[0].Level != overdue.LevelNotice || kept[0].ThresholdDays != 3 {
		t.Fatalf("已生成的预警不应被新规则改写，实际 %+v", kept)
	}
	added := warningsOf(items, fresh.ID)
	if len(added) != 1 || added[0].Level != overdue.LevelCritical || added[0].ThresholdDays != 0 {
		t.Fatalf("新预警应使用新规则快照（严重/0 天），实际 %+v", added)
	}
}

func TestDuplicateRuleOnSameEffectiveDateIsRejected(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	today := date.Today()
	req := overdue.RuleSaveRequest{
		Stage:         overdue.StageFinish,
		ThresholdDays: 2,
		Level:         overdue.LevelWarning,
		EffectiveFrom: today,
	}
	_, err := fixture.Overdue.CreateRule(context.Background(), req)
	testsupport.RequireNoError(t, err)

	_, err = fixture.Overdue.CreateRule(context.Background(), req)
	testsupport.RequireAppError(t, err, httpx.CodeConflict)
}

func TestInvalidRuleStageIsRejected(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	_, err := fixture.Overdue.CreateRule(context.Background(), overdue.RuleSaveRequest{
		Stage:         "unknown",
		ThresholdDays: 1,
		Level:         overdue.LevelNotice,
		EffectiveFrom: date.Today(),
	})
	testsupport.RequireAppError(t, err, httpx.CodeValidation)
}

func TestTaskListOverdueFilterMatchesWarningList(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	today := date.Today()
	overdueTask := createTaskWithPlan(t, fixture, "超期任务", today.AddDays(-6), today.AddDays(2))
	createTaskWithPlan(t, fixture, "正常任务", today.AddDays(-1), today.AddDays(5))

	// 任务列表按超期过滤。
	filtered, total, err := fixture.Tasks.List(context.Background(), cleaningtask.ListQuery{
		Overdue: true,
		Page:    httpx.PageQuery{Page: 1, PageSize: 100},
	})
	testsupport.RequireNoError(t, err)
	if total != 1 || len(filtered) != 1 || filtered[0].ID != overdueTask.ID {
		t.Fatalf("任务列表超期过滤应只返回超期任务，实际 total=%d", total)
	}
	if filtered[0].OverdueWarning == nil || filtered[0].OverdueWarning.Stage != overdue.StageStart {
		t.Fatalf("任务列表项应携带超期预警摘要，实际 %+v", filtered[0].OverdueWarning)
	}

	// 与预警列表口径一致：预警总数 = 超期任务数。
	warnings := activeWarnings(t, fixture)
	if int64(len(warnings)) != total {
		t.Fatalf("预警列表与任务列表口径不一致：预警 %d 条，超期任务 %d 条", len(warnings), total)
	}

	// 按阶段过滤。
	byStage, stageTotal, err := fixture.Tasks.List(context.Background(), cleaningtask.ListQuery{
		OverdueStage: overdue.StageStart,
		Page:         httpx.PageQuery{Page: 1, PageSize: 100},
	})
	testsupport.RequireNoError(t, err)
	if stageTotal != 1 || byStage[0].ID != overdueTask.ID {
		t.Fatalf("按阶段过滤应返回该阶段任务，实际 total=%d", stageTotal)
	}

	// 非法阶段报错。
	_, _, err = fixture.Tasks.List(context.Background(), cleaningtask.ListQuery{OverdueStage: "unknown"})
	testsupport.RequireAppError(t, err, httpx.CodeValidation)
}

func TestDashboardOverviewMatchesWarningSummary(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	today := date.Today()
	createTaskWithPlan(t, fixture, "未按期开工", today.AddDays(-5), today.AddDays(2))
	createTaskWithPlan(t, fixture, "未按期报验", today.AddDays(-10), today.AddDays(-4))

	dashboardSvc := dashboard.NewService(fixture.DB, fixture.Overdue)
	overview, err := dashboardSvc.Overview(context.Background())
	testsupport.RequireNoError(t, err)

	summary, err := fixture.Overdue.Summary(context.Background())
	testsupport.RequireNoError(t, err)

	if overview.TaskOverdue != summary.Total {
		t.Fatalf("看板超期数应与预警统计一致：看板 %d，预警 %d", overview.TaskOverdue, summary.Total)
	}
	if overview.TaskOverdueByStage[overdue.StageStart] != 1 || overview.TaskOverdueByStage[overdue.StageFinish] != 1 {
		t.Fatalf("看板分阶段统计不正确，实际 %+v", overview.TaskOverdueByStage)
	}
}
