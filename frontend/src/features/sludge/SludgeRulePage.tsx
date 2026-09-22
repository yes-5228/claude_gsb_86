// 清淤量换算规则：维护湿重 / 干重 / 体积的折算系数版本链。
//
// 规则 append-only：只能新增更晚生效的版本，不能改写或删除已被引用的历史版本。
// 新增版本前先做影响预览，说明对既有统计的影响范围。
import { useState } from 'react';
import { conversionApi } from '../../api/conversion';
import { toErrorMessage } from '../../api/client';
import { ConfirmDialog } from '../../components/ConfirmDialog';
import { DataTable, type Column } from '../../components/DataTable';
import { PageHeader } from '../../components/PageHeader';
import { SectionCard } from '../../components/SectionCard';
import { StatusTag } from '../../components/StatusTag';
import { StateBlock } from '../../components/StateBlock';
import { useToast } from '../../components/Toast';
import { useAsync } from '../../hooks/useAsync';
import { useMeta } from '../../providers/MetaProvider';
import type { ConversionPair, RuleImpact, SludgeRule } from '../../types/domain';
import { formatDate, formatNumber, formatTonnage, today } from '../../utils/format';
import { optionLabel } from '../../utils/options';

interface FormValues {
  pair: ConversionPair;
  factor: string;
  effectiveFrom: string;
  remark: string;
}

function pairLabel(pair: string): string {
  if (pair === 'm3->wet_t') {
    return '体积 → 湿重（湿污泥密度 t/m³）';
  }
  return '湿重 → 干重（干湿系数）';
}

export function SludgeRulePage() {
  const { enums } = useMeta();
  const toast = useToast();
  const rules = useAsync(() => conversionApi.list(), []);

  const [form, setForm] = useState<FormValues>({
    pair: 'wet_t->dry_t',
    factor: '',
    effectiveFrom: today(),
    remark: ''
  });
  const [impact, setImpact] = useState<RuleImpact | null>(null);
  const [previewing, setPreviewing] = useState(false);
  const [creating, setCreating] = useState(false);
  const [formError, setFormError] = useState('');
  const [pendingDelete, setPendingDelete] = useState<SludgeRule | null>(null);
  const [deleting, setDeleting] = useState(false);

  const factor = Number(form.factor);
  const factorValid = form.factor !== '' && !Number.isNaN(factor) && factor > 0 && factor <= 100;
  const dateValid = /^\d{4}-\d{2}-\d{2}$/.test(form.effectiveFrom);

  const runPreview = async () => {
    if (!factorValid || !dateValid) {
      setFormError('请填写合法的换算系数（大于 0）与生效日期');
      return;
    }
    setFormError('');
    setPreviewing(true);
    try {
      const result = await conversionApi.impact({
        pair: form.pair,
        factor,
        effectiveFrom: form.effectiveFrom
      });
      setImpact(result);
    } catch (cause: unknown) {
      toast.error(toErrorMessage(cause));
      setImpact(null);
    } finally {
      setPreviewing(false);
    }
  };

  const handleCreate = async () => {
    if (!factorValid || !dateValid) {
      setFormError('请填写合法的换算系数（大于 0）与生效日期');
      return;
    }
    setCreating(true);
    try {
      const result = await conversionApi.create({
        pair: form.pair,
        factor,
        effectiveFrom: form.effectiveFrom,
        remark: form.remark.trim()
      });
      toast.success(
        `规则版本已生效：影响 ${result.impact.affectedRecord} 条记录，折算干重合计变化 ${
          result.impact.deltaStandardT >= 0 ? '+' : ''
        }${formatNumber(result.impact.deltaStandardT, 2)} t`
      );
      setImpact(null);
      setForm((prev) => ({ ...prev, factor: '', remark: '' }));
      rules.reload();
    } catch (cause: unknown) {
      toast.error(toErrorMessage(cause));
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async () => {
    if (!pendingDelete) {
      return;
    }
    setDeleting(true);
    try {
      await conversionApi.remove(pendingDelete.id);
      toast.success(`规则版本 ${formatDate(pendingDelete.effectiveFrom)} 已删除`);
      setPendingDelete(null);
      rules.reload();
    } catch (cause: unknown) {
      toast.error(toErrorMessage(cause));
    } finally {
      setDeleting(false);
    }
  };

  const columns: Column<SludgeRule>[] = [
    {
      key: 'pair',
      title: '换算方向',
      render: (row) => (
        <>
          <span className="cell-main">{pairLabel(row.pair)}</span>
          <span className="cell-sub">
            {optionLabel(enums?.sludgeCalibers, row.fromCaliber)} →{' '}
            {optionLabel(enums?.sludgeCalibers, row.toCaliber)}
          </span>
        </>
      )
    },
    { key: 'factor', title: '系数', width: '100px', align: 'right', render: (row) => formatNumber(row.factor, 4) },
    { key: 'effectiveFrom', title: '生效日期', width: '120px', render: (row) => formatDate(row.effectiveFrom) },
    {
      key: 'status',
      title: '状态',
      width: '150px',
      render: (row) => (
        <>
          {row.latest ? <StatusTag list="taskStatuses" value="in_progress" /> : (
            <span className="tag tag-muted">历史版本</span>
          )}
          {row.referenced ? <span className="tag tag-muted" style={{ marginLeft: 6 }}>已引用</span> : null}
        </>
      )
    },
    {
      key: 'affectedRecord',
      title: '覆盖记录',
      width: '100px',
      align: 'right',
      render: (row) => formatNumber(row.affectedRecord, 0)
    },
    { key: 'remark', title: '备注', render: (row) => row.remark || '—' },
    {
      key: 'actions',
      title: '操作',
      width: '90px',
      render: (row) =>
        row.deletable ? (
          <button type="button" className="btn-link" onClick={() => setPendingDelete(row)}>
            删除
          </button>
        ) : (
          <span className="cell-sub">受保护</span>
        )
    }
  ];

  return (
    <div className="page">
      <PageHeader
        title="清淤量换算规则"
        description="维护体积 m³ / 湿重 / 干重之间的换算系数。统一统计口径为干重（干污泥 t）；规则按生效日期形成版本链，折算时取清淤日期当时生效的版本。"
      />

      <StateBlock loading={rules.loading} error={rules.error} onRetry={rules.reload}>
        <SectionCard
          title="规则版本链"
          subtitle="历史版本只增不改；删除仅对尚未覆盖任何清淤记录的未来最新版本开放"
        >
          <div className="card-body-flush">
            <DataTable
              columns={columns}
              rows={rules.data ?? []}
              rowKey={(row) => row.id}
              loading={rules.loading}
              error={rules.error}
              onRetry={rules.reload}
              emptyText="暂无换算规则"
            />
          </div>
        </SectionCard>

        <SectionCard title="新增生效版本" subtitle="新版本生效日期必须晚于该方向的当前最新版本；保存前可先试算影响范围">
          <div className="form-grid">
            <div className="form-field">
              <label className="form-label">换算方向</label>
              <select
                className="select"
                value={form.pair}
                onChange={(event) => {
                  setForm((prev) => ({ ...prev, pair: event.target.value as ConversionPair }));
                  setImpact(null);
                }}
              >
                <option value="m3->wet_t">{pairLabel('m3->wet_t')}</option>
                <option value="wet_t->dry_t">{pairLabel('wet_t->dry_t')}</option>
              </select>
            </div>
            <div className="form-field">
              <label className="form-label">换算系数 *</label>
              <input
                className="input"
                inputMode="decimal"
                placeholder={form.pair === 'm3->wet_t' ? '例如 1.40（t/m³）' : '例如 0.40'}
                value={form.factor}
                onChange={(event) => setForm((prev) => ({ ...prev, factor: event.target.value }))}
              />
            </div>
            <div className="form-field">
              <label className="form-label">生效日期 *</label>
              <input
                className="input"
                type="date"
                value={form.effectiveFrom}
                onChange={(event) => setForm((prev) => ({ ...prev, effectiveFrom: event.target.value }))}
              />
            </div>
            <div className="form-field" style={{ gridColumn: 'span 3' }}>
              <label className="form-label">备注</label>
              <input
                className="input"
                placeholder="例如 三季度污泥标定结果调整"
                value={form.remark}
                onChange={(event) => setForm((prev) => ({ ...prev, remark: event.target.value }))}
              />
            </div>
          </div>
          {formError ? (
            <div className="alert alert-error" style={{ marginTop: 12 }}>
              <p>{formError}</p>
            </div>
          ) : null}
          <div className="form-actions">
            <button type="button" className="btn btn-ghost" disabled={previewing} onClick={() => void runPreview()}>
              {previewing ? '试算中…' : '试算影响范围'}
            </button>
            <button type="button" className="btn btn-primary" disabled={creating} onClick={() => void handleCreate()}>
              {creating ? '保存中…' : '保存新版本'}
            </button>
          </div>

          {impact ? (
            <div className="alert alert-info" style={{ marginTop: 12 }}>
              <p>
                <strong>影响范围（{pairLabel(impact.pair)}，{formatDate(impact.effectiveFrom)} 起生效）</strong>
              </p>
              <p>
                受影响清淤记录 <strong>{formatNumber(impact.affectedRecord, 0)}</strong> 条，涉及{' '}
                <strong>{formatNumber(impact.affectedTask, 0)}</strong> 个任务
                {impact.affectedDistricts.length > 0 ? `，片区：${impact.affectedDistricts.join('、')}` : ''}。
              </p>
              <p>
                受影响记录折算干重：{formatTonnage(impact.beforeStandardT)} → {formatTonnage(impact.afterStandardT)}
                ，变化 <strong>{impact.deltaStandardT >= 0 ? '+' : ''}{formatNumber(impact.deltaStandardT, 2)} t</strong>；
                全系统累计：{formatTonnage(impact.grandTotalBeforeT)} → {formatTonnage(impact.grandTotalAfterT)}。
              </p>
              {impact.samples.length > 0 ? (
                <div className="table-wrap" style={{ marginTop: 8 }}>
                  <table className="data-table">
                    <thead>
                      <tr>
                        <th>记录编号</th>
                        <th>清淤日期</th>
                        <th>片区</th>
                        <th className="align-right">原始值</th>
                        <th className="align-right">调整前（t）</th>
                        <th className="align-right">调整后（t）</th>
                        <th className="align-right">变化（t）</th>
                      </tr>
                    </thead>
                    <tbody>
                      {impact.samples.map((sample) => (
                        <tr key={sample.recordId}>
                          <td>{sample.code}</td>
                          <td>{formatDate(sample.cleanedAt)}</td>
                          <td>{sample.district || '—'}</td>
                          <td className="align-right">
                            {formatNumber(sample.rawAmount, 2)} {sample.caliber === 'm3' ? 'm³' : 't'}
                          </td>
                          <td className="align-right">{formatNumber(sample.beforeT, 2)}</td>
                          <td className="align-right">{formatNumber(sample.afterT, 2)}</td>
                          <td className="align-right">
                            {sample.deltaT >= 0 ? '+' : ''}
                            {formatNumber(sample.deltaT, 2)}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              ) : null}
            </div>
          ) : null}
        </SectionCard>

        <p className="form-note">
          口径说明：体积 m³ 先按湿污泥密度折算为湿重，再按干湿系数折算为干重；湿重记录只乘干湿系数；
          干重记录直接计入统一口径。跨月补录的数据按实际清淤日期（而非录入日期）生效的规则折算。
          所有合计均由记录级折算值求和得到，不在任务 / 管段 / 片区 / 看板各层分别取整。
        </p>
      </StateBlock>

      <ConfirmDialog
        open={pendingDelete !== null}
        title="删除换算规则版本"
        danger
        busy={deleting}
        confirmText="确认删除"
        message={
          <p>
            仅删除未覆盖任何清淤记录的未来版本。版本 {pendingDelete ? formatDate(pendingDelete.effectiveFrom) : ''}{' '}
            删除后不影响既有统计。
          </p>
        }
        onConfirm={handleDelete}
        onCancel={() => setPendingDelete(null)}
      />
    </div>
  );
}
