// 超期预警相关接口。
import type {
  OverdueRulePayload,
  OverdueRulesOverview,
  OverdueSummary,
  OverdueWarningItem,
  PageResult
} from '../types/domain';
import { buildQuery, http } from './client';

export interface WarningQuery {
  stage?: string;
  page?: number;
  pageSize?: number;
}

export const warningApi = {
  list: (query: WarningQuery) => http.get<PageResult<OverdueWarningItem>>(`/overdue-warnings${buildQuery({ ...query })}`),
  summary: () => http.get<OverdueSummary>('/overdue-warnings/summary'),
  rules: () => http.get<OverdueRulesOverview>('/overdue-rules'),
  saveRule: (payload: OverdueRulePayload) => http.post<{ id: number }>('/overdue-rules', payload)
};
