// 换算规则：维护湿重→干重折算系数（按生效时间版本化），新增前可预览对既有统计的影响。
import { useState } from 'react';
import { conversionApi } from '../../api/conversion';
import { DataTable, type Column } from '../../components/DataTable';
import { FormField } from '../../components/FormField';
import { PageHeader } from '../../components/PageHeader';
import { SectionCard } from '../../components/SectionCard';
import { useToast } from '../../components/Toast';
import { useAsync } from '../../hooks/useAsync';
import { useForm, type FormErrors } from '../../hooks/useForm';
import type { ConversionImpact, ConversionLog, RuleView } from '../../types/domain';
import { formatDate, formatFactor, formatNumber, formatWeight } from '../../utils/format';
import { isDateString, today } from '../../utils/format';

interface RuleFormValues {
  name: string;
  effectiveFrom: string;
  wetToDryFactor: string;
  remark: string;
}

function emptyForm(): RuleFormValues {
  return { name: '', effectiveFrom: today(), wetToDryFactor: '', remark: '' };
}

function validate(values: RuleFormValues): FormErrors<RuleFormValues> {
  const errors: FormErrors<RuleFormValues> = {};
  if (!values.name.trim()) {
    errors.name = '规则名称不能为空';
  }
  if (!values.effectiveFrom || !isDateString(values.effectiveFrom)) {
    errors.effectiveFrom = '生效日期格式应为 YYYY-MM-DD';
  }
  const factor = Number(values.wetToDryFactor);
  if (values.wetToDryFactor === '' || Number.isNaN(factor) || factor <= 0 || factor > 1) {
    errors.wetToDryFactor = '折算系数需在 0 ~ 1 之间（不含 0）';
  }
  return errors;
}

export function ConversionRulesPage() {
  const toast = useToast();
  const form = useForm<RuleFormValues>(emptyForm());
  const rules = useAsync(() => conversionApi.rules(), []);
  const logs = useAsync(() => conversionApi.logs(50), []);
  const [impact, setImpact] = useState<ConversionImpact | null>(null);
  const [previewing, setPreviewing] = useState(false);
  const [saving, setSaving] = useState(false);

  const payload = () => ({
    name: form.values.name.trim(),
    effectiveFrom: form.values.effectiveFrom,
    wetToDryFactor: Number(form.values.wetToDryFactor),
    remark: form.values.remark.trim()
  });

  const preview = async () => {
    const errors = validate(form.values);
    if (Object.keys(errors).length > 0) {
      form.setErrors(errors);
      return;
    }
    setPreviewing(true);
    try {
      const result = await conversionApi.impactPreview(payload());
      setImpact(result);
    } catch (cause) {
      toast.error(String((cause as Error)?.message ?? cause));
    } finally {
      setPreviewing(false);
    }
  };

  const submit = async () => {
    const errors = validate(form.values);
    if (Object.keys(errors).length > 0) {
      form.setErrors(errors);
      return;
    }
    setSaving(true);
    try {
      const result = await conversionApi.create(payload());
      toast.success(
        `规则已生效：重算 ${result.impact.affectedRecords} 条记录，区间干重合计 ${formatWeight(
          result.impact.dryBefore
        )} → ${formatWeight(result.impact.dryAfter)}`
      );
      form.reset(emptyForm());
      setImpact(null);
      rules.reload();
      logs.reload();
    } catch (cause) {
      toast.error(String((cause as Error)?.message ?? cause));
    } finally {
      setSaving(false);
    }
  };

  const ruleColumns: Column<RuleView>[] = [
    { key: 'code', title: '规则编号', width: '150px', render: (row) => <span className="cell-main">{row.code}</span> },
    { key: 'name', title: '名称', render: (row) => row.name },
    {
      key: 'effectiveFrom',
      title: '生效区间',
      width: '210px',
      render: (row) => `${formatDate(row.effectiveFrom)} ~ ${row.effectiveTo ? formatDate(row.effectiveTo) : '至今'}`
    },
    {
      key: 'wetToDryFactor',
      title: '湿重→干重系数',
      width: '150px',
      align: 'right',
      render: (row) => formatFactor(row.wetToDryFactor)
    },
    {
      key: 'status',
      title: '状态',
      width: '100px',
      render: (row) => (row.effectiveTo ? <span className="tag tag-muted">历史版本</span> : <span className="tag tag-success">当前生效</span>)
    }
  ];

  const logColumns: Column<ConversionLog>[] = [
    { key: 'createdAt', title: '调整时间', width: '160px', render: (row) => formatDate(row.createdAt) },
    { key: 'ruleCode', title: '规则', width: '150px', render: (row) => <span className="cell-main">{row.ruleCode}</span> },
    {
      key: 'effectiveFrom',
      title: '生效日 / 区间至',
      width: '200px',
      render: (row) => `${formatDate(row.effectiveFrom)} ~ ${row.windowTo ? formatDate(row.windowTo) : '至今'}`
    },
    { key: 'wetToDryFactor', title: '系数', width: '90px', align: 'right', render: (row) => formatFactor(row.wetToDryFactor) },
    { key: 'affectedRecords', title: '重算记录', width: '100px', align: 'right', render: (row) => formatNumber(row.affectedRecords, 0) },
    {
      key: 'dryBefore',
      title: '调整前干重',
      width: '130px',
      align: 'right',
      render: (row) => formatWeight(row.dryBefore)
    },
    { key: 'dryAfter', title: '调整后干重', width: '130px', align: 'right', render: (row) => formatWeight(row.dryAfter) },
    {
      key: 'delta',
      title: '影响量',
      width: '120px',
      align: 'right',
      render: (row) => {
        const delta = row.dryAfter - row.dryBefore;
        return (
          <span className={delta >= 0 ? 'tag tag-success' : 'tag tag-danger'}>
            {delta >= 0 ? '+' : ''}
            {formatWeight(delta)}
          </span>
        );
      }
    }
  ];

  return (
    <div className="page">
      <PageHeader
        title="清淤量换算规则"
        description="湿重 / 干重统一折算为干重（吨）后进入看板与报表。规则按生效时间版本化，新增规则只重算其生效区间内的既有湿重记录，原始计量值不变。"
      />

      <SectionCard title="规则版本" subtitle="同一日期只允许一个版本；清淤记录按清淤日期命中当时生效的规则">
        <div className="card-body-flush">
          <DataTable
            columns={ruleColumns}
            rows={rules.data ?? []}
            rowKey={(row) => row.id}
            loading={rules.loading}
            error={rules.error}
            onRetry={rules.reload}
            emptyText="暂无换算规则"
          />
        </div>
      </SectionCard>

      <SectionCard title="新增规则版本" subtitle="规则只增不改；保存后会在事务内重算生效区间内的既有湿重记录并写入变更日志">
        {form.serverError ? (
          <div className="alert alert-error">
            <p>{form.serverError}</p>
          </div>
        ) : null}
        <div className="form-grid">
          <FormField label="规则名称" required error={form.errors.name}>
            <input className="input" value={form.values.name} onChange={(e) => form.setValue('name', e.target.value)} />
          </FormField>
          <FormField label="生效日期" required error={form.errors.effectiveFrom} hint="自该日（含）起按新系数折算">
            <input
              className="input"
              type="date"
              value={form.values.effectiveFrom}
              onChange={(e) => {
                form.setValue('effectiveFrom', e.target.value);
                setImpact(null);
              }}
            />
          </FormField>
          <FormField label="湿重→干重系数" required error={form.errors.wetToDryFactor} hint="干重（吨）= 湿重（吨）× 系数，范围 (0, 1]">
            <input
              className="input"
              inputMode="decimal"
              placeholder="例如 0.65"
              value={form.values.wetToDryFactor}
              onChange={(e) => {
                form.setValue('wetToDryFactor', e.target.value);
                setImpact(null);
              }}
            />
          </FormField>
          <FormField label="编制依据 / 备注" span={3} error={form.errors.remark}>
            <textarea className="textarea" value={form.values.remark} onChange={(e) => form.setValue('remark', e.target.value)} />
          </FormField>
        </div>

        <div className="form-actions">
          <button type="button" className="btn btn-ghost" disabled={previewing} onClick={() => void preview()}>
            {previewing ? '分析中…' : '预览影响范围'}
          </button>
          <button type="button" className="btn btn-primary" disabled={saving} onClick={() => void submit()}>
            {saving ? '保存中…' : '保存并按区间重算'}
          </button>
        </div>

        {impact ? (
          <div className="alert alert-info">
            <p>
              影响区间：<strong>{formatDate(impact.effectiveFrom)} ~ {impact.windowTo ? formatDate(impact.windowTo) : '至今'}</strong>
              ；将重算湿重记录 <strong>{formatNumber(impact.affectedRecords, 0)}</strong> 条，涉及任务{' '}
              {formatNumber(impact.affectedTasks, 0)} 个、管段 {formatNumber(impact.affectedSegments, 0)} 个、片区{' '}
              {formatNumber(impact.affectedDistricts, 0)} 个。
            </p>
            <p>
              区间折算干重合计：{formatWeight(impact.dryBefore)} → <strong>{formatWeight(impact.dryAfter)}</strong>，
              影响量{' '}
              <span className={impact.delta >= 0 ? '' : ''}>
                {impact.delta >= 0 ? '+' : ''}
                {formatWeight(impact.delta)}
              </span>
              。干重记录系数恒为 1，不在重算范围内；原始计量值不会被改写。
            </p>
          </div>
        ) : null}
      </SectionCard>

      <SectionCard title="规则变更日志" subtitle="每次规则调整对既有统计的影响范围留痕，可用于报表口径说明">
        <div className="card-body-flush">
          <DataTable
            columns={logColumns}
            rows={logs.data ?? []}
            rowKey={(row) => row.id}
            loading={logs.loading}
            error={logs.error}
            onRetry={logs.reload}
            emptyText="暂无规则变更记录"
          />
        </div>
      </SectionCard>
    </div>
  );
}
