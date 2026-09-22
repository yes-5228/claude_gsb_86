import type { Caliber, ConversionImpact, ConversionLog, ConversionRulePayload, RuleView } from '../types/domain';
import { http } from './client';

export const conversionApi = {
  rules: () => http.get<RuleView[]>('/conversion-rules'),
  caliber: () => http.get<Caliber>('/conversion-rules/caliber'),
  logs: (limit = 50) => http.get<ConversionLog[]>(`/conversion-rules/logs?limit=${limit}`),
  impactPreview: (payload: ConversionRulePayload) =>
    http.post<ConversionImpact>('/conversion-rules/impact-preview', payload),
  create: (payload: ConversionRulePayload) =>
    http.post<{ rule: RuleView; impact: ConversionImpact }>('/conversion-rules', payload)
};
