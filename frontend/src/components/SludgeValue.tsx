// 清淤量展示：统一口径（干重 t）为主，原始计量值作为对照同时展示。
//
// 统计口径统一要求界面上原始值与折算值都可见、且原始值不可被改写：
// 折算值以醒目主数字显示，原始值以副文本形式标注口径与单位。
import type { ReactNode } from 'react';
import { useMeta } from '../providers/MetaProvider';
import type { RawAmount, SludgeCaliber } from '../types/domain';
import { formatNumber, formatRawAmount, formatTonnage } from '../utils/format';
import { optionLabel } from '../utils/options';

interface SludgeValueProps {
  /** 原始计量数值。 */
  amount: number;
  /** 原始计量口径。 */
  caliber: SludgeCaliber;
  /** 折算后的干重（干污泥 t）。 */
  standardT: number;
  /** 是否只显示折算主值（空间受限时）。 */
  compact?: boolean;
}

function caliberUnit(caliber: SludgeCaliber): string {
  return caliber === 'm3' ? 'm³' : 't';
}

/** 单条记录的清淤量：折算干重 + 原始口径原值。 */
export function SludgeValue({ amount, caliber, standardT, compact }: SludgeValueProps) {
  const { enums } = useMeta();
  const caliberLabel = optionLabel(enums?.sludgeCalibers, caliber);
  return (
    <>
      <span className="cell-num">{formatTonnage(standardT)}</span>
      {!compact && (
        <span className="cell-sub" title={`原始计量：${caliberLabel}`}>
          原始 {formatRawAmount(amount, caliberUnit(caliber))}
        </span>
      )}
    </>
  );
}

interface RawAmountsProps {
  /** 折算后的统一口径合计。 */
  standardT: number;
  /** 各原始口径合计。 */
  rawAmounts?: RawAmount[] | null;
  empty?: ReactNode;
}

/** 合计场景：折算干重为主，逐口径列出原始值对照。 */
export function StandardWithRaw({ standardT, rawAmounts, empty }: RawAmountsProps) {
  const raws = (rawAmounts ?? []).filter((item) => item.amount > 0);
  return (
    <span className="sludge-dual">
      <span className="sludge-standard">{formatTonnage(standardT)}</span>
      {raws.length > 0 ? (
        <span className="cell-sub">
          原始{' '}
          {raws
            .map((item) => `${formatNumber(item.amount, 2)} ${item.unit || caliberUnit(item.caliber)}`)
            .join(' + ')}
        </span>
      ) : empty ? (
        <span className="cell-sub">{empty}</span>
      ) : null}
    </span>
  );
}
