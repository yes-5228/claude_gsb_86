package cleaningrecord

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/drainage/desilting/internal/httpx"
	"github.com/drainage/desilting/internal/modules/cleaningtask"
	"github.com/drainage/desilting/internal/modules/conversion"
	"github.com/drainage/desilting/internal/shared/date"
	"github.com/drainage/desilting/internal/shared/option"
	"github.com/drainage/desilting/internal/shared/refx"
)

// TaskGateway 清淤任务模块对外提供的能力（由 cleaningtask.Service 实现）。
type TaskGateway interface {
	FindByID(ctx context.Context, id uint) (*cleaningtask.CleaningTask, error)
	EnsureStarted(ctx context.Context, id uint) (*cleaningtask.CleaningTask, error)
}

// ConversionGateway 统一口径折算能力（由 conversion.Service 实现）。
//
// 清淤记录只依赖这一个按业务日期折算的方法，不直接依赖规则表，
// 把「跨月补录按清淤日期当时生效规则折算」的规则集中收敛在换算模块。
type ConversionGateway interface {
	ResolveForRecord(ctx context.Context, rawWeightT float64, basis string, day date.Date) (conversion.Snapshot, error)
}

// Service 清淤记录业务逻辑。
type Service struct {
	repo        *Repository
	tasks       TaskGateway
	conversions ConversionGateway
}

// NewService 构造服务。
func NewService(repo *Repository, tasks TaskGateway, conversions ConversionGateway) *Service {
	return &Service{repo: repo, tasks: tasks, conversions: conversions}
}

// Create 录入清淤记录。首次录入时会把任务从"待开工"推进到"清淤中"。
func (s *Service) Create(ctx context.Context, req SaveRequest) (*CleaningRecord, error) {
	if err := validate(req); err != nil {
		return nil, err
	}
	// 先确认任务可录入（同时完成状态推进），避免写入脏数据。
	if _, err := s.tasks.EnsureStarted(ctx, req.TaskID); err != nil {
		return nil, err
	}

	record := &CleaningRecord{}
	apply(req, record)
	if err := s.fillConversion(ctx, record); err != nil {
		return nil, err
	}

	for attempt := 0; attempt < 5; attempt++ {
		record.Code = s.nextCode(ctx, record.CleanedAt)
		err := s.repo.Create(ctx, record)
		if err == nil {
			return record, nil
		}
		if !errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, httpx.WrapInternal("录入清淤记录失败", err)
		}
	}
	return nil, httpx.Conflict("记录编号生成冲突，请稍后重试")
}

// Update 修改清淤记录。任务进入验收流程后不可再修改。
func (s *Service) Update(ctx context.Context, id uint, req SaveRequest) (*CleaningRecord, error) {
	record, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, notFound(err)
	}
	if req.TaskID != record.TaskID {
		return nil, httpx.InvalidState("清淤记录不支持更换所属任务，如需调整请删除后重新录入")
	}
	task, err := s.tasks.FindByID(ctx, req.TaskID)
	if err != nil {
		return nil, err
	}
	if !editable(task.Status) {
		return nil, httpx.InvalidState(fmt.Sprintf(
			"任务当前状态为「%s」，不能再修改清淤记录", cleaningtask.StatusLabel(task.Status),
		))
	}
	referenced, err := s.repo.HasAcceptance(ctx, id)
	if err != nil {
		return nil, httpx.WrapInternal("检查验收引用失败", err)
	}
	if referenced {
		return nil, httpx.InvalidState("该清淤记录已被验收记录引用，不能再修改")
	}
	if err := validate(req); err != nil {
		return nil, err
	}
	apply(req, record)
	if err := s.fillConversion(ctx, record); err != nil {
		return nil, err
	}
	if err := s.repo.Save(ctx, record); err != nil {
		return nil, httpx.WrapInternal("修改清淤记录失败", err)
	}
	return record, nil
}

// Delete 删除清淤记录。
func (s *Service) Delete(ctx context.Context, id uint) error {
	record, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return notFound(err)
	}
	task, err := s.tasks.FindByID(ctx, record.TaskID)
	if err != nil {
		return err
	}
	if !editable(task.Status) {
		return httpx.InvalidState(fmt.Sprintf(
			"任务当前状态为「%s」，不能再删除清淤记录", cleaningtask.StatusLabel(task.Status),
		))
	}
	referenced, err := s.repo.HasAcceptance(ctx, id)
	if err != nil {
		return httpx.WrapInternal("检查验收引用失败", err)
	}
	if referenced {
		return httpx.InvalidState("该清淤记录已被验收记录引用，不能再删除")
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return notFound(err)
	}
	return nil
}

// FindByID 查询清淤记录。
func (s *Service) FindByID(ctx context.Context, id uint) (*CleaningRecord, error) {
	record, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, notFound(err)
	}
	return record, nil
}

// TotalsByTask 汇总任务下的清淤量（供验收模块判断清淤成果）。
func (s *Service) TotalsByTask(ctx context.Context, taskID uint) (refx.RecordTotals, error) {
	totals, err := s.repo.TotalsByTask(ctx, taskID)
	if err != nil {
		return refx.RecordTotals{}, httpx.WrapInternal("统计清淤量失败", err)
	}
	return totals, nil
}

// List 分页查询清淤记录，并补齐所属任务与管段信息。
func (s *Service) List(ctx context.Context, query ListQuery) ([]ListItem, int64, error) {
	records, total, err := s.repo.List(ctx, query)
	if err != nil {
		return nil, 0, httpx.WrapInternal("查询清淤记录失败", err)
	}
	if len(records) == 0 {
		return []ListItem{}, total, nil
	}

	taskIDs := make([]uint, 0, len(records))
	for i := range records {
		taskIDs = append(taskIDs, records[i].TaskID)
	}
	briefs, err := refx.TaskBriefsByIDs(ctx, s.repo.DB(), taskIDs)
	if err != nil {
		return nil, 0, httpx.WrapInternal("查询任务信息失败", err)
	}

	items := make([]ListItem, 0, len(records))
	for i := range records {
		record := records[i]
		item := ListItem{CleaningRecord: record}
		if brief, ok := briefs[record.TaskID]; ok {
			item.Task = &brief
		}
		items = append(items, item)
	}
	return items, total, nil
}

// Detail 记录详情。
func (s *Service) Detail(ctx context.Context, id uint) (*DetailResponse, error) {
	record, err := s.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	briefs, err := refx.TaskBriefsByIDs(ctx, s.repo.DB(), []uint{record.TaskID})
	if err != nil {
		return nil, httpx.WrapInternal("查询任务信息失败", err)
	}
	detail := &DetailResponse{Record: record}
	if brief, ok := briefs[record.TaskID]; ok {
		detail.Task = &brief
	}
	return detail, nil
}

// editable 判断任务是否处于可以增删改清淤记录的状态。
func editable(taskStatus string) bool {
	return taskStatus == cleaningtask.StatusPending || taskStatus == cleaningtask.StatusInProgress
}

func validate(req SaveRequest) error {
	if req.CleanedAt.IsZero() {
		return httpx.Validation("清淤日期不能为空")
	}
	if req.CleanedAt.After(date.Today()) {
		return httpx.Validation("清淤日期不能晚于今天")
	}
	basis := strings.TrimSpace(req.WeightBasis)
	if !conversion.HasBasis(basis) {
		return httpx.Validation("计量口径只能是湿重或干重")
	}
	if req.RawWeightT <= 0 {
		return httpx.Validation("清淤量原始重量需大于 0（吨）")
	}
	method := strings.TrimSpace(req.Method)
	if method != "" && !option.Has(cleaningtask.MethodOptions(), method) {
		return httpx.Validation(fmt.Sprintf("清淤方式只能是：%s", option.Labels(cleaningtask.MethodOptions())))
	}
	weather := strings.TrimSpace(req.Weather)
	if weather != "" && !option.Has(WeatherOptions(), weather) {
		return httpx.Validation(fmt.Sprintf("天气只能是：%s", option.Labels(WeatherOptions())))
	}
	if strings.TrimSpace(req.RecorderName) == "" {
		return httpx.Validation("记录人不能为空")
	}
	return nil
}

func apply(req SaveRequest, target *CleaningRecord) {
	target.TaskID = req.TaskID
	target.CleanedAt = req.CleanedAt
	target.LengthM = req.LengthM
	target.SludgeVolumeM3 = req.SludgeVolumeM3
	target.RawWeightT = req.RawWeightT
	target.WeightBasis = strings.TrimSpace(req.WeightBasis)
	target.WaterVolumeM3 = req.WaterVolumeM3
	target.PersonnelCount = req.PersonnelCount
	target.Method = strings.TrimSpace(req.Method)
	target.Equipment = strings.TrimSpace(req.Equipment)
	target.Weather = strings.TrimSpace(req.Weather)
	target.SludgeDisposalSite = strings.TrimSpace(req.SludgeDisposalSite)
	target.SafetyMeasures = strings.TrimSpace(req.SafetyMeasures)
	target.ProblemFound = strings.TrimSpace(req.ProblemFound)
	target.RecorderName = strings.TrimSpace(req.RecorderName)
	target.Remark = strings.TrimSpace(req.Remark)
}

// fillConversion 按清淤日期当时生效的规则，把原始重量折算为统一口径干重并固化。
//
// 只写入折算相关列（规则快照、系数、折算干重），原始计量列不在此处理。
// 这样无论是当日录入还是跨月补录，都按业务日期命中规则，口径一致。
func (s *Service) fillConversion(ctx context.Context, record *CleaningRecord) error {
	snap, err := s.conversions.ResolveForRecord(ctx, record.RawWeightT, record.WeightBasis, record.CleanedAt)
	if err != nil {
		return err
	}
	record.ConversionRuleID = snap.RuleID
	record.ConversionRuleCode = snap.RuleCode
	record.ConversionFactor = snap.Factor
	record.ConvertedDryT = snap.DryT
	return nil
}

// nextCode 生成形如 QJ20260914-0001 的记录编号。
func (s *Service) nextCode(ctx context.Context, cleanedAt date.Date) string {
	prefix := "QJ" + cleanedAt.Format("20060102")
	sequence := 1
	if latest, err := s.repo.MaxCodeWithPrefix(ctx, prefix); err == nil && latest != "" {
		if idx := strings.LastIndex(latest, "-"); idx >= 0 {
			if parsed, err := strconv.Atoi(latest[idx+1:]); err == nil {
				sequence = parsed + 1
			}
		}
	}
	return fmt.Sprintf("%s-%04d", prefix, sequence)
}

func notFound(err error) error {
	if errors.Is(err, ErrNotFound) {
		return httpx.NotFound("清淤记录不存在")
	}
	return httpx.WrapInternal("查询清淤记录失败", err)
}
