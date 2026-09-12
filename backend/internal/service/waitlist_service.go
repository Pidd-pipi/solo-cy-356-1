package service

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/communitygarden/server/internal/constants"
	"github.com/communitygarden/server/internal/dto"
	"github.com/communitygarden/server/internal/model"
	"github.com/communitygarden/server/internal/repository"
	"github.com/communitygarden/server/internal/util"
)

// WaitlistService 候补认养服务。
//
// 与 PlotService 的协作：
//   - 地块释放（Release）事务内调用 InviteEarliestWaitingWithTx：最早申请者 waiting -> invited；
//   - 受邀人认养（Adopt）事务内调用 FulfillInviteWithTx：invited -> adopted。
type WaitlistService struct {
	waitRepo repository.WaitlistRepository
	plotRepo repository.PlotRepository
	db       *gorm.DB
	logger   *slog.Logger
}

// NewWaitlistService 构造候补认养服务。
func NewWaitlistService(waitRepo repository.WaitlistRepository, plotRepo repository.PlotRepository, db *gorm.DB, logger *slog.Logger) *WaitlistService {
	return &WaitlistService{waitRepo: waitRepo, plotRepo: plotRepo, db: db, logger: logger}
}

// Apply 市民对已被认养的地块申请候补（事务 + 行锁）。
// 同一地块同一用户仅允许一条有效申请（waiting/invited）。
func (s *WaitlistService) Apply(plotID, userID uint, role, username, note string) (*model.WaitlistEntry, error) {
	var entry *model.WaitlistEntry
	err := s.db.Transaction(func(tx *gorm.DB) error {
		plot, err := s.plotRepo.FindByIDForUpdate(tx, plotID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return util.NewAppError(constants.CodeNotFound, 404, fmt.Sprintf("地块实体 id=%d 不存在", plotID))
			}
			return util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
		}
		// 空闲地块：若已有人持有优先认养资格则拒绝候补并解释；否则直接认养即可。
		if plot.Status == string(constants.PlotStatusAvailable) {
			invited, ierr := s.waitRepo.FindInvitedForUpdate(tx, plotID)
			if ierr != nil && !errors.Is(ierr, repository.ErrNotFound) {
				return util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(ierr)
			}
			if invited != nil {
				return util.NewAppError(constants.CodeWaitlistNotAllowed, 409,
					fmt.Sprintf("地块 %s 释放后的优先认养资格已由候补用户 %d 持有，在其认养或取消前无法申请候补", plot.Code, invited.UserID))
			}
			return util.NewAppError(constants.CodeWaitlistNotAllowed, 409,
				fmt.Sprintf("地块 %s 当前状态为 %s，可直接认养，无需申请候补", plot.Code, util.PlotStatusText(plot.Status)))
		}
		// 当前认养人不能候补自己的地块。
		if plot.AdopterID != nil && *plot.AdopterID == userID {
			return util.NewAppError(constants.CodeWaitlistNotAllowed, 409,
				fmt.Sprintf("用户 %s 已是地块 %s 的认养人，不能申请候补", username, plot.Code))
		}
		existing, err := s.waitRepo.FindActiveByPlotAndUser(tx, plotID, userID)
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			return util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
		}
		if existing != nil {
			return util.NewAppError(constants.CodeWaitlistDuplicate, 409,
				fmt.Sprintf("候补申请实体 user_id=%d 字段 plot_id=%d 已存在状态为 %s 的有效申请", userID, plotID, util.WaitlistStatusText(existing.Status)))
		}
		entry = &model.WaitlistEntry{
			PlotID: plotID,
			UserID: userID,
			Status: string(constants.WaitlistWaiting),
			Note:   note,
		}
		if err := tx.Create(entry).Error; err != nil {
			// 部分唯一索引兜底：并发重复申请在两个驱动上都会返回唯一约束错误。
			if isUniqueConstraintError(err) {
				return util.NewAppError(constants.CodeWaitlistDuplicate, 409,
					fmt.Sprintf("候补申请实体 user_id=%d 字段 plot_id=%d 已存在有效申请", userID, plotID))
			}
			return util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	code := fmt.Sprintf("plot=%d", plotID)
	if plot, perr := s.plotRepo.FindByID(plotID); perr == nil {
		code = plot.Code
	}
	s.logger.Info(constants.LogWaitlistApplied, "waitlist_id", entry.ID, "plot_id", plotID, "code", code, "user_id", userID, "role", role)
	return entry, nil
}

// Cancel 申请人取消自己的候补申请（管理员可代为取消）。
// 取消受邀资格后，立即顺延邀请同地块最早排队者。
func (s *WaitlistService) Cancel(entryID, operatorID uint, operatorRole string) (*model.WaitlistEntry, error) {
	var canceled *model.WaitlistEntry
	var plotCode, fromStatus string
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// 先不加锁读出申请以确定地块，再按“地块行 -> 申请行”顺序加锁，
		// 与 Apply / Release 的加锁顺序保持一致，避免死锁。
		var peek model.WaitlistEntry
		if err := tx.First(&peek, entryID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return util.NewAppError(constants.CodeWaitlistNotFound, 404, fmt.Sprintf("候补申请实体 id=%d 不存在", entryID))
			}
			return util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
		}
		plot, err := s.plotRepo.FindByIDForUpdate(tx, peek.PlotID)
		if err != nil {
			return util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
		}
		plotCode = plot.Code
		entry, err := s.waitRepo.FindByIDForUpdate(tx, entryID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return util.NewAppError(constants.CodeWaitlistNotFound, 404, fmt.Sprintf("候补申请实体 id=%d 不存在", entryID))
			}
			return util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
		}
		if entry.UserID != operatorID && operatorRole != string(constants.RoleAdmin) {
			return util.NewAppError(constants.CodeForbidden, 403,
				fmt.Sprintf("角色 %s 无权取消用户 %d 的候补申请实体 id=%d", util.RoleText(operatorRole), entry.UserID, entryID))
		}
		if entry.Status != string(constants.WaitlistWaiting) && entry.Status != string(constants.WaitlistInvited) {
			return util.NewAppError(constants.CodeWaitlistStateNotAllowed, 409,
				fmt.Sprintf("候补申请实体 id=%d 当前状态为 %s，无法取消", entryID, util.WaitlistStatusText(entry.Status)))
		}
		wasInvited := entry.Status == string(constants.WaitlistInvited)
		fromStatus = entry.Status
		now := time.Now()
		entry.Status = string(constants.WaitlistCancelled)
		entry.CanceledAt = &now
		entry.ResolvedAt = &now
		if err := s.waitRepo.UpdateWithTx(tx, entry); err != nil {
			return util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
		}
		canceled = entry

		// 受邀人放弃资格，且地块仍处于释放后可认养状态时，顺延给下一位。
		if wasInvited && plot.Status == string(constants.PlotStatusAvailable) {
			if err := s.inviteEarliestWaiting(tx, entry.PlotID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.logger.Info(constants.LogWaitlistCancelled, "waitlist_id", canceled.ID, "plot_id", canceled.PlotID,
		"code", plotCode, "user_id", canceled.UserID, "from", fromStatus)
	return canceled, nil
}

// FulfillInviteWithTx 受邀人完成认养时在认养事务内调用：invited -> adopted。
// 非受邀用户认养时由 PlotService 提前拒绝（CodeWaitlistInviteMismatch）。
func (s *WaitlistService) FulfillInviteWithTx(tx *gorm.DB, plotID, userID uint) error {
	entry, err := s.waitRepo.FindInvitedForUpdate(tx, plotID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil // 无有效受邀资格（普通空闲地块认养路径），由调用方保证状态
		}
		return util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
	}
	if entry.UserID != userID {
		return util.NewAppError(constants.CodeWaitlistInviteMismatch, 409,
			fmt.Sprintf("地块 id=%d 的优先认养资格属于用户 %d，用户 %d 暂不能认养", plotID, entry.UserID, userID))
	}
	now := time.Now()
	entry.Status = string(constants.WaitlistAdopted)
	entry.ResolvedAt = &now
	if err := s.waitRepo.UpdateWithTx(tx, entry); err != nil {
		return util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
	}
	if plot, perr := s.plotRepo.FindByIDForUpdate(tx, plotID); perr == nil {
		s.logger.Info(constants.LogWaitlistAdopted, "waitlist_id", entry.ID, "plot_id", plotID, "code", plot.Code, "user_id", userID)
	}
	return nil
}

// InviteEarliestWaitingWithTx 地块释放事务内调用：邀请最早申请者（waiting -> invited）。
// 地块仍有有效受邀资格或无排队者时不做处理。
func (s *WaitlistService) InviteEarliestWaitingWithTx(tx *gorm.DB, plotID uint) error {
	return s.inviteEarliestWaiting(tx, plotID)
}

// inviteEarliestWaiting 将最早排队申请置为受邀，并把地块“预占”给受邀人：
// 地块保持 available，但只有受邀人可以认养（由 Adopt 校验受邀资格）。
func (s *WaitlistService) inviteEarliestWaiting(tx *gorm.DB, plotID uint) error {
	if _, err := s.waitRepo.FindInvitedForUpdate(tx, plotID); err == nil {
		return nil // 已有有效受邀资格，不重复邀请
	} else if !errors.Is(err, repository.ErrNotFound) {
		return util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
	}
	earliest, err := s.waitRepo.FindEarliestWaitingForUpdate(tx, plotID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil // 无人排队
		}
		return util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
	}
	now := time.Now()
	earliest.Status = string(constants.WaitlistInvited)
	earliest.InvitedAt = &now
	if err := s.waitRepo.UpdateWithTx(tx, earliest); err != nil {
		return util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
	}
	code := fmt.Sprintf("plot=%d", plotID)
	if plot, perr := s.plotRepo.FindByIDForUpdate(tx, plotID); perr == nil {
		code = plot.Code
	}
	s.logger.Info(constants.LogWaitlistInvited, "waitlist_id", earliest.ID, "plot_id", plotID, "code", code, "user_id", earliest.UserID)
	return nil
}

// GetInvitedEntry 供 PlotService.Adopt 在事务内调用：返回地块当前受邀资格（无则 nil）。
func (s *WaitlistService) GetInvitedEntry(tx *gorm.DB, plotID uint) (*model.WaitlistEntry, error) {
	entry, err := s.waitRepo.FindInvitedForUpdate(tx, plotID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil
		}
		return nil, util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
	}
	return entry, nil
}

// ListByPlot 查询某地块候补名单（管理员或该地块认养人可查看），按申请时间排序。
func (s *WaitlistService) ListByPlot(plotID, viewerID uint, viewerRole string) ([]*dto.WaitlistOutDTO, int64, error) {
	plot, err := s.plotRepo.FindByID(plotID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, 0, util.NewAppError(constants.CodeNotFound, 404, fmt.Sprintf("地块实体 id=%d 不存在", plotID))
		}
		return nil, 0, util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
	}
	if viewerRole != string(constants.RoleAdmin) && (plot.AdopterID == nil || *plot.AdopterID != viewerID) {
		return nil, 0, util.NewAppError(constants.CodeForbidden, 403,
			fmt.Sprintf("角色 %s 无权查看地块 %s 的候补名单，仅管理员与认养人可查看", util.RoleText(viewerRole), plot.Code))
	}
	entries, err := s.waitRepo.ListByPlot(plotID)
	if err != nil {
		return nil, 0, util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
	}
	out := make([]*dto.WaitlistOutDTO, 0, len(entries))
	waitingRank := 0
	for i := range entries {
		d := dto.ToWaitlistOutDTO(&entries[i])
		if entries[i].Status == string(constants.WaitlistWaiting) {
			waitingRank++
			d.Rank = waitingRank
		}
		out = append(out, d)
	}
	s.logger.Info(constants.LogWaitlistListed, "plot_id", plotID, "viewer", viewerID, "role", viewerRole, "total", len(out))
	return out, int64(len(out)), nil
}

// ListMine 分页查询当前用户的候补申请。
func (s *WaitlistService) ListMine(pq util.PageQuery, userID uint, status string) ([]*dto.WaitlistOutDTO, int64, error) {
	entries, total, err := s.waitRepo.ListByUser(pq, userID, status)
	if err != nil {
		return nil, 0, util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
	}
	out := make([]*dto.WaitlistOutDTO, 0, len(entries))
	for i := range entries {
		d := dto.ToWaitlistOutDTO(&entries[i])
		if entries[i].Status == string(constants.WaitlistWaiting) {
			rank, rerr := s.rankOfWaiting(entries[i].PlotID, entries[i].ID)
			if rerr != nil {
				return nil, 0, rerr
			}
			d.Rank = rank
		}
		out = append(out, d)
	}
	s.logger.Info(constants.LogWaitlistMineQueried, "user_id", userID, "total", total)
	return out, total, nil
}

// Summary 返回地块候补摘要（候补人数 + 当前用户最新申请状态）。
// viewerID 为 0 表示匿名访问，只返回计数。
func (s *WaitlistService) Summary(plotID, viewerID uint) (*dto.WaitlistSummaryDTO, error) {
	if _, err := s.plotRepo.FindByID(plotID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, util.NewAppError(constants.CodeNotFound, 404, fmt.Sprintf("地块实体 id=%d 不存在", plotID))
		}
		return nil, util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
	}
	waiting, active, err := s.waitRepo.CountByPlot(plotID)
	if err != nil {
		return nil, util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
	}
	summary := &dto.WaitlistSummaryDTO{PlotID: plotID, WaitingCount: waiting, ActiveCount: active}
	if viewerID > 0 {
		mine, err := s.waitRepo.FindLatestByPlotAndUser(plotID, viewerID)
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			return nil, util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
		}
		if mine != nil {
			d := dto.ToWaitlistOutDTO(mine)
			if mine.Status == string(constants.WaitlistWaiting) {
				d.Rank, err = s.rankOfWaiting(plotID, mine.ID)
				if err != nil {
					return nil, err
				}
			}
			if mine.Status == string(constants.WaitlistInvited) {
				summary.Reserved = true
				summary.InvitedToMe = true
			}
			summary.Mine = d
		}
	}
	if !summary.Reserved {
		if invitedMap, ierr := s.waitRepo.FindInvitedGrouped([]uint{plotID}); ierr == nil {
			if invited, ok := invitedMap[plotID]; ok {
				summary.Reserved = true
				summary.InvitedToMe = viewerID > 0 && invited.UserID == viewerID
			}
		}
	}
	return summary, nil
}

// rankOfWaiting 计算某排队申请的位次（按申请时间升序，1 开始）。
func (s *WaitlistService) rankOfWaiting(plotID, entryID uint) (int, error) {
	actives, err := s.waitRepo.ListActiveByPlot(plotID)
	if err != nil {
		return 0, util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
	}
	rank := 0
	for i := range actives {
		if actives[i].Status == string(constants.WaitlistWaiting) {
			rank++
		}
		if actives[i].ID == entryID {
			return rank, nil
		}
	}
	return 0, nil
}

// BatchStatus 批量返回多地块的候补计数与当前用户申请状态（地块列表页使用，复用 Summary 同构数据）。
func (s *WaitlistService) BatchStatus(plotIDs []uint, viewerID uint) ([]dto.MyWaitlistStatusDTO, error) {
	waitingMap, err := s.waitRepo.CountWaitingGrouped(plotIDs)
	if err != nil {
		return nil, util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
	}
	activeMap, err := s.waitRepo.CountActiveGrouped(plotIDs)
	if err != nil {
		return nil, util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
	}
	invitedMap, err := s.waitRepo.FindInvitedGrouped(plotIDs)
	if err != nil {
		return nil, util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
	}
	out := make([]dto.MyWaitlistStatusDTO, 0, len(plotIDs))
	for _, pid := range plotIDs {
		item := dto.MyWaitlistStatusDTO{
			PlotID:       pid,
			WaitingCount: waitingMap[pid],
			ActiveCount:  activeMap[pid],
		}
		if invited, ok := invitedMap[pid]; ok {
			item.Reserved = true
			item.InvitedToMe = viewerID > 0 && invited.UserID == viewerID
		}
		if viewerID > 0 {
			mine, merr := s.waitRepo.FindLatestByPlotAndUser(pid, viewerID)
			if merr != nil && !errors.Is(merr, repository.ErrNotFound) {
				return nil, util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(merr)
			}
			if mine != nil && (mine.Status == string(constants.WaitlistWaiting) || mine.Status == string(constants.WaitlistInvited)) {
				item.WaitlistID = mine.ID
				item.Status = mine.Status
				if mine.Status == string(constants.WaitlistWaiting) {
					rank, rerr := s.rankOfWaiting(pid, mine.ID)
					if rerr != nil {
						return nil, rerr
					}
					item.Rank = rank
				}
			}
		}
		out = append(out, item)
	}
	return out, nil
}

// isUniqueConstraintError 识别 PostgreSQL / SQLite 的唯一约束冲突（并发重复申请兜底）。
func isUniqueConstraintError(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || // SQLite
		strings.Contains(msg, "duplicate key value") || // PostgreSQL
		strings.Contains(msg, "23505") // PostgreSQL SQLSTATE
}
