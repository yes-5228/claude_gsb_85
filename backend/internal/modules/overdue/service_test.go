package overdue_test

import (
	"context"
	"testing"
	"time"

	"github.com/drainage/desilting/internal/httpx"
	"github.com/drainage/desilting/internal/modules/cleaningtask"
	"github.com/drainage/desilting/internal/modules/overdue"
	"github.com/drainage/desilting/internal/shared/date"
	"github.com/drainage/desilting/internal/testsupport"
)

func TestDefaultThresholdsApplyWithoutRules(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	thresholds, err := fixture.Overdue.CurrentThresholds(context.Background())
	testsupport.RequireNoError(t, err)
	if thresholds != overdue.DefaultThresholds {
		t.Fatalf("无配置时应回退默认阈值，实际 %+v", thresholds)
	}
}

func TestEnsureDefaultsIsIdempotent(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	ctx := context.Background()
	testsupport.RequireNoError(t, fixture.Overdue.EnsureDefaults(ctx))
	testsupport.RequireNoError(t, fixture.Overdue.EnsureDefaults(ctx))

	overview, err := fixture.Overdue.RulesOverview(ctx)
	testsupport.RequireNoError(t, err)
	if len(overview.Versions) != 3 {
		t.Fatalf("默认配置应只写入一次，实际版本数 %d", len(overview.Versions))
	}
	if len(overview.Current) != 3 {
		t.Fatalf("当前生效配置应覆盖三个阶段，实际 %d", len(overview.Current))
	}

	thresholds, err := fixture.Overdue.CurrentThresholds(ctx)
	testsupport.RequireNoError(t, err)
	if thresholds != overdue.DefaultThresholds {
		t.Fatalf("默认阈值应为 %+v，实际 %+v", overdue.DefaultThresholds, thresholds)
	}
}

func TestSaveRuleRejectsInvalidInput(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	ctx := context.Background()

	_, err := fixture.Overdue.SaveRule(ctx, overdue.SaveRuleRequest{
		Stage: "unknown", ThresholdDays: 3, EffectiveFrom: date.Today(),
	})
	testsupport.RequireAppError(t, err, httpx.CodeValidation)

	_, err = fixture.Overdue.SaveRule(ctx, overdue.SaveRuleRequest{
		Stage: overdue.StageNotStarted, ThresholdDays: -1, EffectiveFrom: date.Today(),
	})
	testsupport.RequireAppError(t, err, httpx.CodeValidation)

	// 生效日期早于今天：调整只能影响之后的判定，不允许回溯
	_, err = fixture.Overdue.SaveRule(ctx, overdue.SaveRuleRequest{
		Stage: overdue.StageNotStarted, ThresholdDays: 3, EffectiveFrom: date.Today().AddDays(-1),
	})
	testsupport.RequireAppError(t, err, httpx.CodeValidation)
}

func TestSaveRuleOverridesSameEffectiveDate(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	ctx := context.Background()
	today := date.Today()

	_, err := fixture.Overdue.SaveRule(ctx, overdue.SaveRuleRequest{
		Stage: overdue.StageNotStarted, ThresholdDays: 5, EffectiveFrom: today,
	})
	testsupport.RequireNoError(t, err)
	_, err = fixture.Overdue.SaveRule(ctx, overdue.SaveRuleRequest{
		Stage: overdue.StageNotStarted, ThresholdDays: 7, EffectiveFrom: today,
	})
	testsupport.RequireNoError(t, err)

	overview, err := fixture.Overdue.RulesOverview(ctx)
	testsupport.RequireNoError(t, err)
	if len(overview.Versions) != 1 {
		t.Fatalf("同阶段同生效日期应覆盖而非新增版本，实际版本数 %d", len(overview.Versions))
	}

	thresholds, err := fixture.Overdue.CurrentThresholds(ctx)
	testsupport.RequireNoError(t, err)
	if thresholds.NotStartedDays != 7 {
		t.Fatalf("同一天重复保存应以后一次为准，实际 %d", thresholds.NotStartedDays)
	}
}

func TestFutureRuleDoesNotAffectCurrentJudgement(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	ctx := context.Background()

	// 明天才生效的阈值不参与今天的判定
	_, err := fixture.Overdue.SaveRule(ctx, overdue.SaveRuleRequest{
		Stage: overdue.StageNotStarted, ThresholdDays: 30, EffectiveFrom: date.Today().AddDays(1),
	})
	testsupport.RequireNoError(t, err)

	thresholds, err := fixture.Overdue.CurrentThresholds(ctx)
	testsupport.RequireNoError(t, err)
	if thresholds.NotStartedDays != overdue.DefaultThresholds.NotStartedDays {
		t.Fatalf("未来生效的阈值不应影响当前判定，实际 %d", thresholds.NotStartedDays)
	}
}

func TestPendingTaskFallsIntoNotStarted(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	task := createTaskWithPlan(t, fixture, "迟迟未开工的任务", -5, 2)

	item := findOnlyWarning(t, fixture, task.ID)
	if item.Stage != overdue.StageNotStarted {
		t.Fatalf("应落入未开工阶段，实际 %q", item.Stage)
	}
	if item.Level != overdue.LevelNotice {
		t.Fatalf("未开工应为提示级别，实际 %q", item.Level)
	}
	if item.OverdueDays != 5 {
		t.Fatalf("超期天数应为 5，实际 %d", item.OverdueDays)
	}
}

func TestPendingTaskPastPlanEndFallsIntoNotReportedOnly(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	// 计划开始与计划完成都已过期：只能落入更严重的未报验阶段，不能重复提醒
	task := createTaskWithPlan(t, fixture, "计划完成已过仍未开工", -10, -4)

	items, total := listWarnings(t, fixture, "")
	if total != 1 || len(items) != 1 {
		t.Fatalf("同一任务只能出现一次，实际总数 %d 条数 %d", total, len(items))
	}
	if items[0].TaskID != task.ID {
		t.Fatalf("预警任务应为 %d，实际 %d", task.ID, items[0].TaskID)
	}
	if items[0].Stage != overdue.StageNotReported {
		t.Fatalf("应只落入未报验阶段，实际 %q", items[0].Stage)
	}
	if items[0].Level != overdue.LevelWarning {
		t.Fatalf("未报验应为警告级别，实际 %q", items[0].Level)
	}
	if items[0].OverdueDays != 4 {
		t.Fatalf("超期天数应为 4，实际 %d", items[0].OverdueDays)
	}
}

func TestInProgressTaskPastPlanEndFallsIntoNotReported(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	task := createTaskWithPlan(t, fixture, "清淤中超期未报验", -10, -4)
	testsupport.RequireNoError(t, ignoreTask(fixture.Tasks.Start(context.Background(), task.ID)))

	item := findOnlyWarning(t, fixture, task.ID)
	if item.Stage != overdue.StageNotReported {
		t.Fatalf("应落入未报验阶段，实际 %q", item.Stage)
	}
}

func TestCompletedTaskWaitingAcceptanceFallsIntoNotAccepted(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	task := fixture.TaskReadyForAcceptance(t, fixture.Segment.ID, "报验后长期未验收")
	// 把报验时间拨到 10 天前，超过默认的 7 天阈值
	finishedAt := date.Today().AddDays(-10).Time.Add(10 * time.Hour)
	testsupport.RequireNoError(t, fixture.DB.Model(&cleaningtask.CleaningTask{}).
		Where("id = ?", task.ID).Update("finished_at", finishedAt).Error)

	item := findOnlyWarning(t, fixture, task.ID)
	if item.Stage != overdue.StageNotAccepted {
		t.Fatalf("应落入未验收阶段，实际 %q", item.Stage)
	}
	if item.Level != overdue.LevelCritical {
		t.Fatalf("未验收应为严重级别，实际 %q", item.Level)
	}
	if item.OverdueDays != 10 {
		t.Fatalf("超期天数应为 10，实际 %d", item.OverdueDays)
	}
}

func TestCompletedTaskWithinThresholdIsNotWarning(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	fixture.TaskReadyForAcceptance(t, fixture.Segment.ID, "刚报验的任务")

	_, total := listWarnings(t, fixture, "")
	if total != 0 {
		t.Fatalf("报验未满阈值不应预警，实际 %d 条", total)
	}
}

func TestAcceptedAndCancelledTasksAreExcluded(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	ctx := context.Background()

	// 已验收：即使计划日期早已过期也不参与超期统计
	accepted := fixture.TaskReadyForAcceptance(t, fixture.Segment.ID, "已验收的任务")
	_, err := fixture.Acceptances.Create(ctx, testsupport.PassRequest(accepted.ID, 90))
	testsupport.RequireNoError(t, err)

	// 已取消：同样不参与
	cancelled := createTaskWithPlan(t, fixture, "已取消的任务", -10, -4)
	testsupport.RequireNoError(t, ignoreTask(fixture.Tasks.Cancel(ctx, cancelled.ID, "计划调整")))

	_, total := listWarnings(t, fixture, "")
	if total != 0 {
		t.Fatalf("已验收与已取消的任务不应参与超期统计，实际 %d 条", total)
	}
}

func TestSummaryMatchesWarningList(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	ctx := context.Background()

	createTaskWithPlan(t, fixture, "未开工任务", -5, 2)
	createTaskWithPlan(t, fixture, "未报验任务", -10, -4)
	completed := fixture.TaskReadyForAcceptance(t, fixture.Segment.ID, "未验收任务")
	finishedAt := date.Today().AddDays(-10).Time.Add(10 * time.Hour)
	testsupport.RequireNoError(t, fixture.DB.Model(&cleaningtask.CleaningTask{}).
		Where("id = ?", completed.ID).Update("finished_at", finishedAt).Error)

	total, byStage, err := fixture.Overdue.CountByStage(ctx)
	testsupport.RequireNoError(t, err)
	if total != 3 {
		t.Fatalf("预警总数应为 3，实际 %d", total)
	}
	if byStage[overdue.StageNotStarted] != 1 ||
		byStage[overdue.StageNotReported] != 1 ||
		byStage[overdue.StageNotAccepted] != 1 {
		t.Fatalf("分阶段计数应为各 1，实际 %+v", byStage)
	}

	// 预警列表与汇总口径一致
	_, listTotal := listWarnings(t, fixture, "")
	if listTotal != total {
		t.Fatalf("预警列表与汇总口径不一致：列表 %d，汇总 %d", listTotal, total)
	}

	// 按阶段过滤
	items, stageTotal := listWarnings(t, fixture, overdue.StageNotReported)
	if stageTotal != 1 || len(items) != 1 || items[0].Stage != overdue.StageNotReported {
		t.Fatalf("按阶段过滤应只返回未报验任务，实际总数 %d", stageTotal)
	}
}

func TestThresholdChangeAffectsJudgement(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	ctx := context.Background()
	// 计划开始已过 5 天：默认阈值 3 天应预警
	createTaskWithPlan(t, fixture, "未开工 5 天的任务", -5, 2)

	if _, total := listWarnings(t, fixture, ""); total != 1 {
		t.Fatalf("默认阈值下应预警，实际 %d 条", total)
	}

	// 把未开工阈值调宽到 10 天（今天生效）后，同一任务不再预警
	_, err := fixture.Overdue.SaveRule(ctx, overdue.SaveRuleRequest{
		Stage: overdue.StageNotStarted, ThresholdDays: 10, EffectiveFrom: date.Today(),
	})
	testsupport.RequireNoError(t, err)

	if _, total := listWarnings(t, fixture, ""); total != 0 {
		t.Fatalf("阈值调宽后不应再预警，实际 %d 条", total)
	}
}

// createTaskWithPlan 创建指定计划日期的待开工任务。
func createTaskWithPlan(t *testing.T, fixture *testsupport.Fixture, title string, startOffset, endOffset int) *cleaningtask.CleaningTask {
	t.Helper()
	task, err := fixture.Tasks.Create(context.Background(), cleaningtask.SaveRequest{
		Title:         title,
		PipeSegmentID: fixture.Segment.ID,
		PlanStartDate: date.Today().AddDays(startOffset),
		PlanEndDate:   date.Today().AddDays(endOffset),
	})
	testsupport.RequireNoError(t, err)
	return task
}

// listWarnings 查询预警列表（单页全量）。
func listWarnings(t *testing.T, fixture *testsupport.Fixture, stage string) ([]overdue.WarningItem, int64) {
	t.Helper()
	items, total, err := fixture.Overdue.ListWarnings(context.Background(), overdue.ListQuery{
		Stage: stage,
		Page:  httpx.PageQuery{Page: 1, PageSize: 50},
	})
	testsupport.RequireNoError(t, err)
	return items, total
}

// findOnlyWarning 断言只有一条预警并返回它。
func findOnlyWarning(t *testing.T, fixture *testsupport.Fixture, taskID uint) overdue.WarningItem {
	t.Helper()
	items, total := listWarnings(t, fixture, "")
	if total != 1 || len(items) != 1 {
		t.Fatalf("期望只有 1 条预警，实际总数 %d 条数 %d", total, len(items))
	}
	if items[0].TaskID != taskID {
		t.Fatalf("预警任务 ID 应为 %d，实际 %d", taskID, items[0].TaskID)
	}
	return items[0]
}

// ignoreTask 丢弃任务返回值，只保留 error，便于在断言里直接使用。
func ignoreTask(_ *cleaningtask.CleaningTask, err error) error {
	return err
}
