// 与后端接口一一对应的领域类型定义。

export type PipeType = 'rainwater' | 'sewage' | 'combined';
export type SegmentStatus = 'normal' | 'attention' | 'blocked';
export type TaskStatus = 'pending' | 'in_progress' | 'completed' | 'accepted' | 'cancelled';
export type TaskPriority = 'low' | 'normal' | 'high' | 'urgent';
export type TaskSource = 'plan' | 'inspection' | 'complaint' | 'flood';
export type CleaningMethod = 'high_pressure' | 'winch' | 'grab' | 'manual' | 'robot';
export type Weather = 'sunny' | 'cloudy' | 'overcast' | 'light_rain' | 'heavy_rain';
export type AcceptanceResult = 'pass' | 'rework';
/** 清淤量原始计量口径：体积 m³ / 湿重（湿污泥 t）/ 干重（干污泥 t）。 */
export type SludgeCaliber = 'm3' | 'wet_t' | 'dry_t';
/** 换算规则方向。 */
export type ConversionPair = 'm3->wet_t' | 'wet_t->dry_t';

/** 任务可执行的操作标识，由后端 allowedActions 下发。 */
export type TaskAction = 'start' | 'complete' | 'accept' | 'cancel' | 'edit';

export interface Option {
  value: string;
  label: string;
}

export interface PageResult<T> {
  list: T[];
  total: number;
  page: number;
  pageSize: number;
}

// ---------- 管段台账 ----------

export interface PipeSegment {
  id: number;
  code: string;
  name: string;
  district: string;
  roadName: string;
  pipeType: PipeType;
  material: string;
  diameterMm: number;
  lengthM: number;
  depthM: number;
  startManhole: string;
  endManhole: string;
  buildYear: number;
  ownerUnit: string;
  status: SegmentStatus;
  lastCleanedAt: string | null;
  cleanedTimes: number;
  remark: string;
  createdAt: string;
  updatedAt: string;
}

export interface SegmentBrief {
  id: number;
  code: string;
  name: string;
  district: string;
  roadName: string;
}

export interface TaskStats {
  total: number;
  pending: number;
  inProgress: number;
  completed: number;
  accepted: number;
  cancelled: number;
}

export interface TaskRef {
  id: number;
  code: string;
  title: string;
  status: TaskStatus;
  priority: TaskPriority;
  teamName: string;
  planStartDate: string | null;
  planEndDate: string | null;
  recordCount: number;
  standardSludgeT: number;
  rawAmounts: RawAmount[];
}

/** 某原始口径下的数值合计（不换算，仅用于对照原始台账）。 */
export interface RawAmount {
  caliber: SludgeCaliber;
  unit: string;
  amount: number;
}

export interface SegmentDetail {
  segment: PipeSegment;
  taskStats: TaskStats;
  recentTasks: TaskRef[];
}

export interface SegmentHistoryItem {
  taskId: number;
  taskCode: string;
  title: string;
  status: TaskStatus;
  priority: TaskPriority;
  teamName: string;
  planStartDate: string | null;
  planEndDate: string | null;
  recordCount: number;
  standardSludgeT: number;
  rawAmounts: RawAmount[];
  cleanedLengthM: number;
  acceptanceResult: AcceptanceResult | '';
  acceptedAt: string | null;
}

export interface SegmentOptions {
  items: SegmentBrief[];
  districts: string[];
}

export interface SegmentPayload {
  code: string;
  name: string;
  district: string;
  roadName: string;
  pipeType: PipeType;
  material: string;
  diameterMm: number;
  lengthM: number;
  depthM: number;
  startManhole: string;
  endManhole: string;
  buildYear: number;
  ownerUnit: string;
  status?: SegmentStatus;
  remark: string;
}

// ---------- 清淤任务 ----------

export interface CleaningTask {
  id: number;
  code: string;
  title: string;
  pipeSegmentId: number;
  priority: TaskPriority;
  source: TaskSource;
  method: CleaningMethod | '';
  planStartDate: string | null;
  planEndDate: string | null;
  teamName: string;
  leaderName: string;
  leaderPhone: string;
  status: TaskStatus;
  description: string;
  startedAt: string | null;
  finishedAt: string | null;
  acceptedAt: string | null;
  cancelReason: string;
  createdAt: string;
  updatedAt: string;
}

export interface RecordTotals {
  recordCount: number;
  /** 折算到统一口径（干重 t）后的合计，任务/管段/片区/看板四层一致。 */
  standardSludgeT: number;
  /** 各原始口径数值合计（不参与跨口径汇总，仅对照展示）。 */
  rawAmounts: RawAmount[];
  cleanedLengthM: number;
  latestCleanedAt: string | null;
}

export interface AcceptanceBrief {
  id: number;
  code: string;
  result: AcceptanceResult;
  acceptedAt: string | null;
  inspectorName: string;
  inspectorOrg: string;
  score: number;
  issues: string;
  rectifyDeadline: string | null;
  rectifiedAt: string | null;
}

export interface TaskListItem extends CleaningTask {
  segment: SegmentBrief | null;
  recordTotals: RecordTotals;
}

export interface TaskDetail {
  task: CleaningTask;
  segment: SegmentBrief | null;
  recordTotals: RecordTotals;
  acceptance: AcceptanceBrief | null;
  allowedActions: TaskAction[];
}

export interface TaskPayload {
  title: string;
  pipeSegmentId: number;
  priority: TaskPriority;
  source: TaskSource;
  method: CleaningMethod | '';
  planStartDate: string;
  planEndDate: string;
  teamName: string;
  leaderName: string;
  leaderPhone: string;
  description: string;
}

// ---------- 清淤记录 ----------

export interface TaskBrief {
  id: number;
  code: string;
  title: string;
  status: TaskStatus;
  priority: TaskPriority;
  pipeSegmentId: number;
  teamName: string;
  segmentCode: string;
  segmentName: string;
  segmentDistrict: string;
}

export interface CleaningRecord {
  id: number;
  code: string;
  taskId: number;
  cleanedAt: string | null;
  lengthM: number;
  /** 原始计量数值（现场录入，不被折算改写）。 */
  sludgeAmount: number;
  /** 原始计量口径：m3 / wet_t / dry_t。 */
  sludgeCaliber: SludgeCaliber;
  /** 按清淤日期当时生效规则折算的干重（干污泥 t）。 */
  standardT: number;
  waterVolumeM3: number;
  personnelCount: number;
  method: CleaningMethod | '';
  equipment: string;
  weather: Weather | '';
  sludgeDisposalSite: string;
  safetyMeasures: string;
  problemFound: string;
  recorderName: string;
  remark: string;
  createdAt: string;
  updatedAt: string;
}

export interface RecordListItem extends CleaningRecord {
  task: TaskBrief | null;
}

export interface RecordDetail {
  record: CleaningRecord;
  task: TaskBrief | null;
}

export interface RecordPayload {
  taskId: number;
  cleanedAt: string;
  lengthM: number;
  sludgeAmount: number;
  sludgeCaliber: SludgeCaliber;
  waterVolumeM3: number;
  personnelCount: number;
  method: CleaningMethod | '';
  equipment: string;
  weather: Weather | '';
  sludgeDisposalSite: string;
  safetyMeasures: string;
  problemFound: string;
  recorderName: string;
  remark: string;
}

// ---------- 验收记录 ----------

export interface AcceptanceRecord {
  id: number;
  code: string;
  taskId: number;
  cleaningRecordId: number | null;
  acceptedAt: string | null;
  inspectorName: string;
  inspectorOrg: string;
  result: AcceptanceResult;
  score: number;
  residualSludgeMm: number;
  issues: string;
  rectification: string;
  rectifyDeadline: string | null;
  rectifiedAt: string | null;
  remark: string;
  createdAt: string;
  updatedAt: string;
}

export interface AcceptanceListItem extends AcceptanceRecord {
  task: TaskBrief | null;
}

export interface AcceptanceDetail {
  acceptance: AcceptanceRecord;
  task: TaskBrief | null;
  recordTotals: RecordTotals;
}

export interface AcceptancePayload {
  taskId: number;
  cleaningRecordId: number | null;
  acceptedAt: string;
  inspectorName: string;
  inspectorOrg: string;
  result: AcceptanceResult;
  score: number;
  residualSludgeMm: number;
  issues: string;
  rectification: string;
  rectifyDeadline: string | null;
  remark: string;
}

export interface RectifyPayload {
  rectifiedAt: string;
  rectification: string;
  remark: string;
}

// ---------- 看板与元数据 ----------

export interface Overview {
  segmentTotal: number;
  segmentTotalLengthM: number;
  segmentByStatus: Record<string, number>;
  uncleanedSegmentCount: number;
  taskTotal: number;
  taskByStatus: Record<string, number>;
  taskOverdue: number;
  recordTotal: number;
  /** 累计折算干重（干污泥 t，统一口径）。 */
  sludgeTotalT: number;
  sludgeThisMonthT: number;
  cleanedLengthM: number;
  acceptanceTotal: number;
  acceptancePassCount: number;
  /** 验收合格率，后端已按百分比返回（66.67 表示 66.67%）。 */
  acceptancePassRate: number;
  pendingAcceptanceCount: number;
  pendingRectifyCount: number;
}

export interface DistrictStat {
  district: string;
  segmentCount: number;
  segmentLengthM: number;
  uncleanedSegmentCount: number;
  lastCleanedAt: string | null;
  taskCount: number;
  acceptedTaskCount: number;
  standardSludgeT: number;
}

export interface PendingAcceptanceItem {
  taskId: number;
  code: string;
  title: string;
  segmentCode: string;
  segmentName: string;
  segmentDistrict: string;
  teamName: string;
  planEndDate: string | null;
  finishedAt: string | null;
  recordCount: number;
  standardSludgeT: number;
  overdueDays: number;
}

export interface RecentRecordItem {
  recordId: number;
  code: string;
  cleanedAt: string | null;
  taskId: number;
  taskCode: string;
  taskTitle: string;
  segmentCode: string;
  segmentName: string;
  teamName: string;
  recorderName: string;
  lengthM: number;
  sludgeAmount: number;
  sludgeCaliber: SludgeCaliber;
  standardT: number;
}

export interface Enums {
  pipeTypes: Option[];
  segmentStatuses: Option[];
  materials: Option[];
  taskStatuses: Option[];
  taskPriorities: Option[];
  taskSources: Option[];
  cleaningMethods: Option[];
  weathers: Option[];
  acceptanceResults: Option[];
  sludgeCalibers: Option[];
}

// ---------- 清淤量换算规则 ----------

export interface SludgeRule {
  id: number;
  pair: ConversionPair;
  factor: number;
  effectiveFrom: string;
  remark: string;
  createdAt: string;
  updatedAt: string;
  fromCaliber: SludgeCaliber;
  toCaliber: SludgeCaliber;
  latest: boolean;
  referenced: boolean;
  deletable: boolean;
  affectedRecord: number;
}

export interface SludgeRulePayload {
  pair: ConversionPair;
  factor: number;
  effectiveFrom: string;
  remark: string;
}

export interface RuleImpactSample {
  recordId: number;
  code: string;
  taskId: number;
  cleanedAt: string;
  district: string;
  caliber: SludgeCaliber;
  rawAmount: number;
  beforeT: number;
  afterT: number;
  deltaT: number;
}

export interface RuleImpact {
  pair: ConversionPair;
  factor: number;
  effectiveFrom: string;
  affectedRecord: number;
  affectedTask: number;
  affectedDistricts: string[];
  beforeStandardT: number;
  afterStandardT: number;
  deltaStandardT: number;
  grandTotalBeforeT: number;
  grandTotalAfterT: number;
  samples: RuleImpactSample[];
}

export interface ConversionPreview {
  amount: number;
  caliber: SludgeCaliber;
  cleanedAt: string;
  standardT: number;
  densityTPerM3: number;
  wetToDryFactor: number;
  factorSource: string;
}
