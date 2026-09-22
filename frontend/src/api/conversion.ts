import type {
  ConversionPreview,
  RuleImpact,
  SludgeCaliber,
  SludgeRule,
  SludgeRulePayload
} from '../types/domain';
import { http } from './client';

export interface ImpactRequest {
  pair: string;
  factor: number;
  effectiveFrom: string;
}

export const conversionApi = {
  list: () => http.get<SludgeRule[]>('/sludge-rules'),
  create: (payload: SludgeRulePayload) =>
    http.post<{ rule: SludgeRule; impact: RuleImpact }>('/sludge-rules', payload),
  impact: (payload: ImpactRequest) => http.post<RuleImpact>('/sludge-rules/impact', payload),
  preview: (payload: { amount: number; caliber: SludgeCaliber; cleanedAt: string }) =>
    http.post<ConversionPreview>('/sludge-rules/preview', payload),
  remove: (id: number) => http.del<{ id: number }>(`/sludge-rules/${id}`)
};
