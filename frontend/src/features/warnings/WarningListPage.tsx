// 超期预警：按未开工、未报验、未验收三个阶段跟踪任务超期，并维护各阶段判定阈值。
import { useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { toErrorMessage } from '../../api/client';
import { warningApi } from '../../api/warnings';
import { DataTable, type Column } from '../../components/DataTable';
import { PageHeader } from '../../components/PageHeader';
import { Pagination } from '../../components/Pagination';
import { SectionCard } from '../../components/SectionCard';
import { StatCard } from '../../components/StatCard';
import { StatusTag } from '../../components/StatusTag';
import { useToast } from '../../components/Toast';
import { useAsync } from '../../hooks/useAsync';
import { useMeta } from '../../providers/MetaProvider';
import type { OverdueRuleCurrent, OverdueRuleVersion, OverdueStage, OverdueWarningItem } from '../../types/domain';
import { formatDate, formatDateTime, formatNumber, today } from '../../utils/format';
import { optionLabel } from '../../utils/options';

const PAGE_SIZE = 10;

const warningColumns: Column<OverdueWarningItem>[] = [
  {
    key: 'code',
    title: '任务编号',
    width: '150px',
    render: (row) => (
      <>
        <Link className="cell-main" to={`/tasks/${row.taskId}`}>
          {row.code}
        </Link>
        <span className="cell-sub">{row.title}</span>
      </>
    )
  },
  {
    key: 'segment',
    title: '关联管段',
    width: '170px',
    render: (row) => (
      <>
        <span>{row.segmentCode || '—'}</span>
        <span className="cell-sub">{row.segmentName ? `${row.district} · ${row.segmentName}` : '管段已删除'}</span>
      </>
    )
  },
  { key: 'stage', title: '预警阶段', width: '100px', render: (row) => <StatusTag list="overdueStages" value={row.stage} /> },
  { key: 'level', title: '级别', width: '90px', render: (row) => <StatusTag list="overdueLevels" value={row.level} /> },
  {
    key: 'overdueDays',
    title: '超期天数',
    width: '100px',
    align: 'right',
    render: (row) => <span className="cell-num">{formatNumber(row.overdueDays, 0)} 天</span>
  },
  {
    key: 'plan',
    title: '计划周期',
    width: '190px',
    render: (row) => `${formatDate(row.planStartDate)} ~ ${formatDate(row.planEndDate)}`
  },
  { key: 'teamName', title: '实施班组', width: '120px', render: (row) => row.teamName || '—' },
  {
    key: 'action',
    title: '操作',
    width: '90px',
    render: (row) => (
      <Link className="link" to={`/tasks/${row.taskId}`}>
        查看任务
      </Link>
    )
  }
];

const currentRuleColumns: Column<OverdueRuleCurrent>[] = [
  { key: 'stage', title: '预警阶段', render: (row) => <StatusTag list="overdueStages" value={row.stage} /> },
  { key: 'level', title: '级别', width: '100px', render: (row) => <StatusTag list="overdueLevels" value={row.level} /> },
  {
    key: 'thresholdDays',
    title: '阈值',
    width: '140px',
    align: 'right',
    render: (row) => <span className="cell-num">超过 {formatNumber(row.thresholdDays, 0)} 天</span>
  },
  {
    key: 'effectiveFrom',
    title: '生效日期',
    width: '140px',
    render: (row) => (row.effectiveFrom ? formatDate(row.effectiveFrom) : <span className="tag tag-muted">系统默认</span>)
  }
];

export function WarningListPage() {
  const toast = useToast();
  const { enums } = useMeta();
  const [params, setParams] = useSearchParams();

  const stage = params.get('stage') ?? '';
  const page = Math.max(1, Number(params.get('page') ?? '1') || 1);

  const summary = useAsync(() => warningApi.summary(), []);
  const rules = useAsync(() => warningApi.rules(), []);
  const list = useAsync(() => warningApi.list({ stage, page, pageSize: PAGE_SIZE }), [stage, page]);

  const [ruleStage, setRuleStage] = useState<OverdueStage>('not_started');
  const [ruleDays, setRuleDays] = useState('3');
  const [ruleEffective, setRuleEffective] = useState(today());
  const [saving, setSaving] = useState(false);

  const applyFilter = (patch: Record<string, string>) => {
    const next = new URLSearchParams(params);
    Object.entries(patch).forEach(([key, value]) => {
      if (value) {
        next.set(key, value);
      } else {
        next.delete(key);
      }
    });
    next.set('page', '1');
    setParams(next);
  };

  const goPage = (nextPage: number) => {
    const next = new URLSearchParams(params);
    next.set('page', String(nextPage));
    setParams(next);
  };

  const handleSaveRule = async () => {
    const days = Number(ruleDays);
    if (!Number.isInteger(days) || days < 0 || days > 365) {
      toast.error('预警阈值需为 0-365 之间的整数');
      return;
    }
    if (!ruleEffective) {
      toast.error('请选择生效日期');
      return;
    }
    setSaving(true);
    try {
      await warningApi.saveRule({ stage: ruleStage, thresholdDays: days, effectiveFrom: ruleEffective });
      toast.success('阈值配置已保存，自生效日期起参与判定');
      rules.reload();
      summary.reload();
      list.reload();
    } catch (cause: unknown) {
      toast.error(toErrorMessage(cause));
    } finally {
      setSaving(false);
    }
  };

  const byStage = summary.data?.byStage ?? {};
  const stageLabel = (value: OverdueStage) => optionLabel(enums?.overdueStages, value);

  const versionColumns: Column<OverdueRuleVersion>[] = [
    {
      key: 'stage',
      title: '预警阶段',
      render: (row) => <StatusTag list="overdueStages" value={row.stage} />
    },
    {
      key: 'thresholdDays',
      title: '阈值',
      width: '140px',
      align: 'right',
      render: (row) => <span className="cell-num">超过 {formatNumber(row.thresholdDays, 0)} 天</span>
    },
    { key: 'effectiveFrom', title: '生效日期', width: '140px', render: (row) => formatDate(row.effectiveFrom) },
    { key: 'createdAt', title: '创建时间', width: '170px', render: (row) => formatDateTime(row.createdAt) }
  ];

  return (
    <div className="page">
      <PageHeader
        title="超期预警"
        description="按未开工、未报验、未验收三个阶段跟踪任务超期；已取消与已验收的任务不参与统计，同一任务只会落入一个阶段。"
      />

      <div className="stat-grid">
        <StatCard
          label="预警总数"
          value={formatNumber(summary.data?.total ?? 0, 0)}
          hint="处于任一预警阶段的任务数"
          tone={summary.data && summary.data.total > 0 ? 'danger' : 'success'}
          onClick={() => applyFilter({ stage: '' })}
        />
        <StatCard
          label={stageLabel('not_started')}
          value={formatNumber(byStage.not_started ?? 0, 0)}
          hint="计划开始时间已到仍未开工"
          tone={(byStage.not_started ?? 0) > 0 ? 'primary' : 'default'}
          onClick={() => applyFilter({ stage: 'not_started' })}
        />
        <StatCard
          label={stageLabel('not_reported')}
          value={formatNumber(byStage.not_reported ?? 0, 0)}
          hint="计划完成时间已过仍未报验"
          tone={(byStage.not_reported ?? 0) > 0 ? 'warn' : 'default'}
          onClick={() => applyFilter({ stage: 'not_reported' })}
        />
        <StatCard
          label={stageLabel('not_accepted')}
          value={formatNumber(byStage.not_accepted ?? 0, 0)}
          hint="报验之后长期没有验收结论"
          tone={(byStage.not_accepted ?? 0) > 0 ? 'danger' : 'default'}
          onClick={() => applyFilter({ stage: 'not_accepted' })}
        />
      </div>

      <SectionCard title="预警列表" subtitle={`共 ${list.data?.total ?? 0} 条预警`}>
        <div className="card-body-flush">
          <div className="filter-bar">
            <div className="filter-item">
              <span className="filter-label">预警阶段</span>
              <select className="select" value={stage} onChange={(event) => applyFilter({ stage: event.target.value })}>
                <option value="">全部阶段</option>
                {(enums?.overdueStages ?? []).map((item) => (
                  <option key={item.value} value={item.value}>
                    {item.label}
                  </option>
                ))}
              </select>
            </div>
            <div className="filter-actions">
              <button type="button" className="btn btn-ghost" onClick={() => setParams(new URLSearchParams())}>
                重置
              </button>
            </div>
          </div>

          <DataTable
            columns={warningColumns}
            rows={list.data?.list ?? []}
            rowKey={(row) => row.taskId}
            loading={list.loading}
            error={list.error}
            onRetry={list.reload}
            emptyText="暂无超期预警"
            emptyDescription="当前没有任务落入任何预警阶段。"
          />
          <Pagination total={list.data?.total ?? 0} page={page} pageSize={list.data?.pageSize ?? PAGE_SIZE} onChange={goPage} />
        </div>
      </SectionCard>

      <SectionCard title="阈值配置" subtitle="每个阶段按生效日期取最新配置，调整只影响之后的判定，历史口径不变">
        <div className="card-body-flush">
          <DataTable
            columns={currentRuleColumns}
            rows={rules.data?.current ?? []}
            rowKey={(row) => row.stage}
            loading={rules.loading}
            error={rules.error}
            onRetry={rules.reload}
            emptyText="暂无阈值配置"
          />

          <div className="filter-bar">
            <div className="filter-item">
              <span className="filter-label">预警阶段</span>
              <select
                className="select"
                value={ruleStage}
                onChange={(event) => setRuleStage(event.target.value as OverdueStage)}
              >
                {(enums?.overdueStages ?? []).map((item) => (
                  <option key={item.value} value={item.value}>
                    {item.label}
                  </option>
                ))}
              </select>
            </div>
            <div className="filter-item">
              <span className="filter-label">阈值（天）</span>
              <input
                className="input"
                type="number"
                min={0}
                max={365}
                value={ruleDays}
                onChange={(event) => setRuleDays(event.target.value)}
              />
            </div>
            <div className="filter-item">
              <span className="filter-label">生效日期</span>
              <input
                className="input"
                type="date"
                min={today()}
                value={ruleEffective}
                onChange={(event) => setRuleEffective(event.target.value)}
              />
            </div>
            <div className="filter-actions">
              <button type="button" className="btn btn-primary" disabled={saving} onClick={handleSaveRule}>
                {saving ? '保存中…' : '保存配置'}
              </button>
            </div>
          </div>

          <p className="form-note" style={{ padding: '0 16px' }}>
            调整历史（同一阶段同一生效日期重复保存会覆盖原配置）
          </p>
          <DataTable
            columns={versionColumns}
            rows={rules.data?.versions ?? []}
            rowKey={(row) => row.id}
            emptyText="暂无调整记录"
          />
        </div>
      </SectionCard>
    </div>
  );
}
