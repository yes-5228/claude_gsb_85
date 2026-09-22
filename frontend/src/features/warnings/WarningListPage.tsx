// 超期预警：分阶段预警列表 + 阈值规则配置。
//
// 列表、任务列表与看板共用后端同一套超期口径；规则带生效日期，
// 调整只影响之后的判定，已生成的预警保持当时的阈值与级别快照。
import { useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { toErrorMessage } from '../../api/client';
import { warningApi } from '../../api/warnings';
import { DataTable, type Column } from '../../components/DataTable';
import { FormField } from '../../components/FormField';
import { Modal } from '../../components/Modal';
import { PageHeader } from '../../components/PageHeader';
import { Pagination } from '../../components/Pagination';
import { SectionCard } from '../../components/SectionCard';
import { StatCard } from '../../components/StatCard';
import { StatusTag } from '../../components/StatusTag';
import { useToast } from '../../components/Toast';
import { useAsync } from '../../hooks/useAsync';
import { useMeta } from '../../providers/MetaProvider';
import type { OverdueRulePayload, OverdueWarningItem } from '../../types/domain';
import { formatDate, formatDateTime, formatNumber, today } from '../../utils/format';
import { optionLabel } from '../../utils/options';

const PAGE_SIZE = 10;

const emptyRuleForm: OverdueRulePayload = {
  stage: '',
  thresholdDays: 0,
  level: '',
  effectiveFrom: today()
};

export function WarningListPage() {
  const toast = useToast();
  const { enums } = useMeta();
  const [params, setParams] = useSearchParams();

  const stage = params.get('stage') ?? '';
  const level = params.get('level') ?? '';
  const status = params.get('status') ?? 'active';
  const keyword = params.get('keyword') ?? '';
  const page = Math.max(1, Number(params.get('page') ?? '1') || 1);

  const [keywordInput, setKeywordInput] = useState(keyword);
  const list = useAsync(
    () => warningApi.list({ stage, level, status, keyword, page, pageSize: PAGE_SIZE }),
    [stage, level, status, keyword, page]
  );
  const summary = useAsync(() => warningApi.summary(), []);
  const rules = useAsync(() => warningApi.rules(), []);

  const [ruleModalOpen, setRuleModalOpen] = useState(false);
  const [ruleForm, setRuleForm] = useState<OverdueRulePayload>(emptyRuleForm);
  const [savingRule, setSavingRule] = useState(false);

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

  const reloadAll = () => {
    list.reload();
    summary.reload();
    rules.reload();
  };

  const handleCreateRule = async () => {
    if (!ruleForm.stage || !ruleForm.level) {
      toast.error('请选择预警阶段与提示级别');
      return;
    }
    if (!ruleForm.effectiveFrom) {
      toast.error('请选择生效日期');
      return;
    }
    setSavingRule(true);
    try {
      await warningApi.createRule(ruleForm);
      toast.success('预警规则已保存，将按生效日期参与后续判定');
      setRuleModalOpen(false);
      setRuleForm(emptyRuleForm);
      reloadAll();
    } catch (cause: unknown) {
      toast.error(toErrorMessage(cause));
    } finally {
      setSavingRule(false);
    }
  };

  const columns: Column<OverdueWarningItem>[] = [
    {
      key: 'task',
      title: '任务',
      width: '200px',
      render: (row) => (
        <>
          <Link className="cell-main" to={`/tasks/${row.taskId}`}>
            {row.taskCode}
          </Link>
          <span className="cell-sub">{row.taskTitle}</span>
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
          <span className="cell-sub">{row.district ? `${row.district} · ${row.segmentName}` : row.segmentName || '—'}</span>
        </>
      )
    },
    { key: 'stage', title: '超期阶段', width: '110px', render: (row) => <StatusTag list="overdueStages" value={row.stage} /> },
    { key: 'level', title: '提示级别', width: '90px', render: (row) => <StatusTag list="overdueLevels" value={row.level} /> },
    {
      key: 'overdueDays',
      title: '超期天数',
      width: '90px',
      align: 'right',
      render: (row) => <span className="cell-num">{formatNumber(row.overdueDays, 0)} 天</span>
    },
    {
      key: 'thresholdDays',
      title: '触发阈值',
      width: '90px',
      align: 'right',
      render: (row) => `${formatNumber(row.thresholdDays, 0)} 天`
    },
    {
      key: 'status',
      title: '状态',
      width: '150px',
      render: (row) => (
        <>
          <StatusTag list="overdueWarningStatuses" value={row.status} />
          {row.status === 'resolved' && row.resolveReason ? <span className="cell-sub">{row.resolveReason}</span> : null}
        </>
      )
    },
    {
      key: 'createdAt',
      title: '首次提醒',
      width: '140px',
      render: (row) => formatDateTime(row.createdAt)
    }
  ];

  const summaryData = summary.data;
  const todayText = today();
  // 每个阶段当前生效的规则：生效日期不晚于今天的最新一条（rules 接口已按生效日期倒序返回）。
  const effectiveRuleByStage = new Map<string, { thresholdDays: number; level: string }>();
  (rules.data ?? []).forEach((rule) => {
    if (rule.effectiveFrom <= todayText && !effectiveRuleByStage.has(rule.stage)) {
      effectiveRuleByStage.set(rule.stage, { thresholdDays: rule.thresholdDays, level: rule.level });
    }
  });

  return (
    <div className="page">
      <PageHeader
        title="超期预警"
        description="按未按期开工、未按期报验、验收超期三个阶段跟踪任务超期，已取消与已验收的任务不参与统计。"
        actions={
          <button type="button" className="btn btn-primary" onClick={() => setRuleModalOpen(true)}>
            新增预警规则
          </button>
        }
      />

      <div className="stat-grid">
        <StatCard
          label="待处理预警"
          value={formatNumber(summaryData?.total ?? 0, 0)}
          hint="与任务列表、运行看板同一口径"
          tone={summaryData && summaryData.total > 0 ? 'warn' : 'success'}
          onClick={() => applyFilter({ stage: '', status: 'active' })}
        />
        {(enums?.overdueStages ?? []).map((item) => {
          const rule = effectiveRuleByStage.get(item.value);
          return (
            <StatCard
              key={item.value}
              label={item.label}
              value={formatNumber(summaryData?.byStage?.[item.value] ?? 0, 0)}
              hint={
                rule
                  ? `阈值 ${formatNumber(rule.thresholdDays, 0)} 天 · ${optionLabel(enums?.overdueLevels, rule.level)}`
                  : '未配置规则'
              }
              tone={item.value === 'accept' ? 'warn' : 'default'}
              onClick={() => applyFilter({ stage: item.value, status: 'active' })}
            />
          );
        })}
      </div>

      <SectionCard title="预警列表" subtitle={`共 ${list.data?.total ?? 0} 条记录`}>
        <div className="card-body-flush">
          <div className="filter-bar">
            <div className="filter-item" style={{ minWidth: 200 }}>
              <span className="filter-label">关键字</span>
              <input
                className="input"
                placeholder="任务编号 / 标题"
                value={keywordInput}
                onChange={(event) => setKeywordInput(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') {
                    applyFilter({ keyword: keywordInput });
                  }
                }}
              />
            </div>
            <div className="filter-item">
              <span className="filter-label">超期阶段</span>
              <select className="select" value={stage} onChange={(event) => applyFilter({ stage: event.target.value })}>
                <option value="">全部阶段</option>
                {(enums?.overdueStages ?? []).map((item) => (
                  <option key={item.value} value={item.value}>
                    {item.label}
                  </option>
                ))}
              </select>
            </div>
            <div className="filter-item">
              <span className="filter-label">提示级别</span>
              <select className="select" value={level} onChange={(event) => applyFilter({ level: event.target.value })}>
                <option value="">全部级别</option>
                {(enums?.overdueLevels ?? []).map((item) => (
                  <option key={item.value} value={item.value}>
                    {item.label}
                  </option>
                ))}
              </select>
            </div>
            <div className="filter-item">
              <span className="filter-label">状态</span>
              <select className="select" value={status} onChange={(event) => applyFilter({ status: event.target.value })}>
                {(enums?.overdueWarningStatuses ?? []).map((item) => (
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
              <button type="button" className="btn btn-primary" onClick={() => applyFilter({ keyword: keywordInput })}>
                查询
              </button>
            </div>
          </div>

          <DataTable
            columns={columns}
            rows={list.data?.list ?? []}
            rowKey={(row) => row.id}
            loading={list.loading}
            error={list.error}
            onRetry={list.reload}
            emptyText="暂无符合条件的预警"
            emptyDescription="任务状态推进或规则调整后，预警会自动生成或解除。"
          />
          <Pagination total={list.data?.total ?? 0} page={page} pageSize={list.data?.pageSize ?? PAGE_SIZE} onChange={goPage} />
        </div>
      </SectionCard>

      <SectionCard
        title="预警规则"
        subtitle="每个阶段取生效日期不晚于今天的最新规则参与判定；调整只影响之后生成的预警"
      >
        <div className="card-body-flush">
          <DataTable
            columns={[
              { key: 'stage', title: '超期阶段', render: (row) => <StatusTag list="overdueStages" value={row.stage} /> },
              { key: 'level', title: '提示级别', width: '110px', render: (row) => <StatusTag list="overdueLevels" value={row.level} /> },
              {
                key: 'thresholdDays',
                title: '触发阈值',
                width: '140px',
                render: (row) => `超过计划时间 ${formatNumber(row.thresholdDays, 0)} 天`
              },
              { key: 'effectiveFrom', title: '生效日期', width: '120px', render: (row) => formatDate(row.effectiveFrom) },
              { key: 'createdAt', title: '配置时间', width: '150px', render: (row) => formatDateTime(row.createdAt) }
            ]}
            rows={rules.data ?? []}
            rowKey={(row) => row.id}
            loading={rules.loading}
            error={rules.error}
            onRetry={rules.reload}
            emptyText="暂无预警规则"
          />
        </div>
      </SectionCard>

      <Modal
        open={ruleModalOpen}
        title="新增预警规则"
        onClose={() => setRuleModalOpen(false)}
        footer={
          <>
            <button type="button" className="btn btn-ghost" onClick={() => setRuleModalOpen(false)}>
              取消
            </button>
            <button type="button" className="btn btn-primary" disabled={savingRule} onClick={handleCreateRule}>
              {savingRule ? '保存中…' : '保存规则'}
            </button>
          </>
        }
      >
        <div className="form-grid">
          <FormField label="预警阶段" required hint="同一阶段同一生效日期只能配置一次">
            <select
              className="select"
              value={ruleForm.stage}
              onChange={(event) => setRuleForm({ ...ruleForm, stage: event.target.value as OverdueRulePayload['stage'] })}
            >
              <option value="">请选择阶段</option>
              {(enums?.overdueStages ?? []).map((item) => (
                <option key={item.value} value={item.value}>
                  {item.label}
                </option>
              ))}
            </select>
          </FormField>
          <FormField label="提示级别" required>
            <select
              className="select"
              value={ruleForm.level}
              onChange={(event) => setRuleForm({ ...ruleForm, level: event.target.value as OverdueRulePayload['level'] })}
            >
              <option value="">请选择级别</option>
              {(enums?.overdueLevels ?? []).map((item) => (
                <option key={item.value} value={item.value}>
                  {item.label}
                </option>
              ))}
            </select>
          </FormField>
          <FormField label="触发阈值（天）" required hint="超过计划时间多少天后触发，0 表示计划日次日即触发">
            <input
              className="input"
              type="number"
              min={0}
              max={365}
              value={ruleForm.thresholdDays}
              onChange={(event) => setRuleForm({ ...ruleForm, thresholdDays: Number(event.target.value) || 0 })}
            />
          </FormField>
          <FormField label="生效日期" required hint="可填写未来日期预约生效；只影响生效之后的判定">
            <input
              className="input"
              type="date"
              value={ruleForm.effectiveFrom}
              onChange={(event) => setRuleForm({ ...ruleForm, effectiveFrom: event.target.value })}
            />
          </FormField>
        </div>
      </Modal>
    </div>
  );
}
