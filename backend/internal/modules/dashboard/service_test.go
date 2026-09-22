package dashboard_test

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

func TestOverviewOverdueConsistentWithWarnings(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	ctx := context.Background()

	// 未开工：计划开始已过 5 天（默认阈值 3 天）
	createTask(t, fixture, "未开工任务", -5, 2)
	// 未报验：计划完成已过 4 天（默认阈值 0 天）
	createTask(t, fixture, "未报验任务", -10, -4)
	// 未验收：报验已过 10 天（默认阈值 7 天）
	completed := fixture.TaskReadyForAcceptance(t, fixture.Segment.ID, "未验收任务")
	finishedAt := date.Today().AddDays(-10).Time.Add(10 * time.Hour)
	testsupport.RequireNoError(t, fixture.DB.Model(&cleaningtask.CleaningTask{}).
		Where("id = ?", completed.ID).Update("finished_at", finishedAt).Error)
	// 已取消、已验收不参与超期统计
	cancelled := createTask(t, fixture, "已取消任务", -10, -4)
	testsupport.RequireNoError(t, ignoreTask(fixture.Tasks.Cancel(ctx, cancelled.ID, "汛期调度调整")))
	accepted := fixture.TaskReadyForAcceptance(t, fixture.Segment.ID, "已验收任务")
	_, err := fixture.Acceptances.Create(ctx, testsupport.PassRequest(accepted.ID, 88))
	testsupport.RequireNoError(t, err)

	svc := dashboard.NewService(fixture.DB, fixture.Overdue)
	overview, err := svc.Overview(ctx)
	testsupport.RequireNoError(t, err)

	if overview.TaskOverdue != 3 {
		t.Fatalf("看板超期总数应为 3，实际 %d", overview.TaskOverdue)
	}
	if overview.TaskOverdueByStage[overdue.StageNotStarted] != 1 ||
		overview.TaskOverdueByStage[overdue.StageNotReported] != 1 ||
		overview.TaskOverdueByStage[overdue.StageNotAccepted] != 1 {
		t.Fatalf("看板分阶段超期应为各 1，实际 %+v", overview.TaskOverdueByStage)
	}

	// 看板与预警列表口径一致
	_, listTotal, err := fixture.Overdue.ListWarnings(ctx, overdue.ListQuery{
		Page: httpx.PageQuery{Page: 1, PageSize: 50},
	})
	testsupport.RequireNoError(t, err)
	if listTotal != overview.TaskOverdue {
		t.Fatalf("看板与预警列表口径不一致：看板 %d，列表 %d", overview.TaskOverdue, listTotal)
	}
}

func TestPendingAcceptanceOverdueDaysFollowsThreshold(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	ctx := context.Background()

	recent := fixture.TaskReadyForAcceptance(t, fixture.Segment.ID, "刚报验的任务")
	old := fixture.TaskReadyForAcceptance(t, fixture.Segment.ID, "报验很久的任务")
	finishedAt := date.Today().AddDays(-10).Time.Add(10 * time.Hour)
	testsupport.RequireNoError(t, fixture.DB.Model(&cleaningtask.CleaningTask{}).
		Where("id = ?", old.ID).Update("finished_at", finishedAt).Error)

	svc := dashboard.NewService(fixture.DB, fixture.Overdue)
	items, err := svc.PendingAcceptance(ctx, 10)
	testsupport.RequireNoError(t, err)

	days := make(map[uint]int, len(items))
	for _, item := range items {
		days[item.TaskID] = item.OverdueDays
	}
	if days[recent.ID] != 0 {
		t.Fatalf("未超过阈值的待验收任务不应计超期，实际 %d", days[recent.ID])
	}
	if days[old.ID] != 10 {
		t.Fatalf("超过阈值的待验收任务超期天数应为 10，实际 %d", days[old.ID])
	}
}

// createTask 创建指定计划日期的待开工任务。
func createTask(t *testing.T, fixture *testsupport.Fixture, title string, startOffset, endOffset int) *cleaningtask.CleaningTask {
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

// ignoreTask 丢弃任务返回值，只保留 error，便于在断言里直接使用。
func ignoreTask(_ *cleaningtask.CleaningTask, err error) error {
	return err
}
