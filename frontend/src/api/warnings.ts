import type { OverdueRule, OverdueRulePayload, OverdueSummary, OverdueWarningItem, PageResult } from '../types/domain';
import { buildQuery, http } from './client';

export interface WarningQuery {
  stage?: string;
  level?: string;
  status?: string;
  keyword?: string;
  page?: number;
  pageSize?: number;
}

export const warningApi = {
  list: (query: WarningQuery) => http.get<PageResult<OverdueWarningItem>>(`/overdue-warnings${buildQuery({ ...query })}`),
  summary: () => http.get<OverdueSummary>('/overdue-warnings/summary'),
  rules: () => http.get<OverdueRule[]>('/overdue-rules'),
  createRule: (payload: OverdueRulePayload) => http.post<OverdueRule>('/overdue-rules', payload)
};
