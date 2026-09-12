package repository

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/communitygarden/server/internal/constants"
	"github.com/communitygarden/server/internal/model"
	"github.com/communitygarden/server/internal/util"
)

// WaitlistRepository 候补认养仓储接口。
type WaitlistRepository interface {
	Create(e *model.WaitlistEntry) error
	FindByID(id uint) (*model.WaitlistEntry, error)
	// FindByIDForUpdate 按主键锁定申请行（事务内执行；SQLite 下行锁子句被忽略）。
	FindByIDForUpdate(tx *gorm.DB, id uint) (*model.WaitlistEntry, error)
	// FindActiveByPlotAndUser 查询某用户在某地块的有效申请（waiting/invited），事务内执行。
	FindActiveByPlotAndUser(tx *gorm.DB, plotID, userID uint) (*model.WaitlistEntry, error)
	// FindLatestByPlotAndUser 查询某用户在某地块最近一条申请（含终态，用于状态展示）。
	FindLatestByPlotAndUser(plotID, userID uint) (*model.WaitlistEntry, error)
	// FindInvitedForUpdate 查询地块当前持有的优先认养资格（行锁，事务内）。
	FindInvitedForUpdate(tx *gorm.DB, plotID uint) (*model.WaitlistEntry, error)
	// FindEarliestWaitingForUpdate 查询最早排队申请（created_at ASC，行锁，事务内）。
	FindEarliestWaitingForUpdate(tx *gorm.DB, plotID uint) (*model.WaitlistEntry, error)
	// ListActiveByPlot 按申请时间升序返回有效申请（waiting/invited，无预载）。
	ListActiveByPlot(plotID uint) ([]model.WaitlistEntry, error)
	// ListByPlot 返回某地块全部候补申请（含终态，预载申请人，按申请时间升序）。
	ListByPlot(plotID uint) ([]model.WaitlistEntry, error)
	// ListByUser 分页查询某用户的申请，预载地块。
	ListByUser(pq util.PageQuery, userID uint, status string) ([]model.WaitlistEntry, int64, error)
	// CountByPlot 返回排队人数（waiting）与有效申请人数（waiting+invited）。
	CountByPlot(plotID uint) (waiting int64, active int64, err error)
	// CountWaitingGrouped 按地块分组统计 waiting 人数。
	CountWaitingGrouped(plotIDs []uint) (map[uint]int64, error)
	// CountActiveGrouped 按地块分组统计有效申请（waiting+invited）人数。
	CountActiveGrouped(plotIDs []uint) (map[uint]int64, error)
	// FindInvitedGrouped 按地块分组返回当前受邀申请（每地块至多一条）。
	FindInvitedGrouped(plotIDs []uint) (map[uint]model.WaitlistEntry, error)
	UpdateWithTx(tx *gorm.DB, e *model.WaitlistEntry) error
}

type waitlistRepository struct {
	db *gorm.DB
}

// NewWaitlistRepository 构造候补认养仓储。
func NewWaitlistRepository(db *gorm.DB) WaitlistRepository {
	return &waitlistRepository{db: db}
}

func (r *waitlistRepository) Create(e *model.WaitlistEntry) error {
	return r.db.Create(e).Error
}

func (r *waitlistRepository) FindByID(id uint) (*model.WaitlistEntry, error) {
	var e model.WaitlistEntry
	if err := r.db.Preload("Plot").Preload("User").First(&e, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &e, nil
}

func (r *waitlistRepository) FindByIDForUpdate(tx *gorm.DB, id uint) (*model.WaitlistEntry, error) {
	var e model.WaitlistEntry
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&e, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &e, nil
}

func (r *waitlistRepository) FindActiveByPlotAndUser(tx *gorm.DB, plotID, userID uint) (*model.WaitlistEntry, error) {
	var e model.WaitlistEntry
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("plot_id = ? AND user_id = ? AND status IN ?", plotID, userID, constants.WaitlistActiveStatuses).
		First(&e).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &e, nil
}

func (r *waitlistRepository) FindLatestByPlotAndUser(plotID, userID uint) (*model.WaitlistEntry, error) {
	var e model.WaitlistEntry
	err := r.db.Where("plot_id = ? AND user_id = ?", plotID, userID).
		Order("created_at DESC, id DESC").First(&e).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &e, nil
}

func (r *waitlistRepository) FindInvitedForUpdate(tx *gorm.DB, plotID uint) (*model.WaitlistEntry, error) {
	var e model.WaitlistEntry
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("plot_id = ? AND status = ?", plotID, string(constants.WaitlistInvited)).
		First(&e).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &e, nil
}

func (r *waitlistRepository) FindEarliestWaitingForUpdate(tx *gorm.DB, plotID uint) (*model.WaitlistEntry, error) {
	var e model.WaitlistEntry
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("plot_id = ? AND status = ?", plotID, string(constants.WaitlistWaiting)).
		Order("created_at ASC, id ASC").First(&e).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &e, nil
}

func (r *waitlistRepository) ListActiveByPlot(plotID uint) ([]model.WaitlistEntry, error) {
	var list []model.WaitlistEntry
	err := r.db.Where("plot_id = ? AND status IN ?", plotID, constants.WaitlistActiveStatuses).
		Order("created_at ASC, id ASC").Find(&list).Error
	return list, err
}

// waitlistOrder 候补名单按申请时间升序（created_at ASC；id ASC 打破同时间并列）。
const waitlistOrder = "created_at ASC, id ASC"

func (r *waitlistRepository) ListByPlot(plotID uint) ([]model.WaitlistEntry, error) {
	var list []model.WaitlistEntry
	err := r.db.Preload("User").
		Where("plot_id = ? AND status IN ?", plotID, constants.WaitlistActiveStatuses).
		Order(waitlistOrder).Find(&list).Error
	return list, err
}

func (r *waitlistRepository) ListByUser(pq util.PageQuery, userID uint, status string) ([]model.WaitlistEntry, int64, error) {
	var list []model.WaitlistEntry
	var total int64
	q := r.db.Model(&model.WaitlistEntry{}).Where("user_id = ?", userID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := util.Paginate(q.Preload("Plot").Order("created_at DESC, id DESC"), pq).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (r *waitlistRepository) CountByPlot(plotID uint) (int64, int64, error) {
	var waiting, active int64
	if err := r.db.Model(&model.WaitlistEntry{}).Where("plot_id = ? AND status = ?", plotID, string(constants.WaitlistWaiting)).Count(&waiting).Error; err != nil {
		return 0, 0, err
	}
	if err := r.db.Model(&model.WaitlistEntry{}).Where("plot_id = ? AND status IN ?", plotID, constants.WaitlistActiveStatuses).Count(&active).Error; err != nil {
		return 0, 0, err
	}
	return waiting, active, nil
}

func countGrouped(db *gorm.DB, plotIDs []uint, statuses []constants.WaitlistStatus) (map[uint]int64, error) {
	type row struct {
		PlotID uint
		Count  int64
	}
	var rows []row
	if err := db.Model(&model.WaitlistEntry{}).
		Select("plot_id, count(*) as count").
		Where("plot_id IN ? AND status IN ?", plotIDs, statuses).
		Group("plot_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[uint]int64, len(rows))
	for _, v := range rows {
		out[v.PlotID] = v.Count
	}
	return out, nil
}

func (r *waitlistRepository) CountWaitingGrouped(plotIDs []uint) (map[uint]int64, error) {
	if len(plotIDs) == 0 {
		return map[uint]int64{}, nil
	}
	return countGrouped(r.db, plotIDs, []constants.WaitlistStatus{constants.WaitlistWaiting})
}

func (r *waitlistRepository) CountActiveGrouped(plotIDs []uint) (map[uint]int64, error) {
	if len(plotIDs) == 0 {
		return map[uint]int64{}, nil
	}
	return countGrouped(r.db, plotIDs, constants.WaitlistActiveStatuses)
}

func (r *waitlistRepository) FindInvitedGrouped(plotIDs []uint) (map[uint]model.WaitlistEntry, error) {
	out := make(map[uint]model.WaitlistEntry)
	if len(plotIDs) == 0 {
		return out, nil
	}
	var list []model.WaitlistEntry
	if err := r.db.Where("plot_id IN ? AND status = ?", plotIDs, string(constants.WaitlistInvited)).
		Find(&list).Error; err != nil {
		return nil, err
	}
	for i := range list {
		out[list[i].PlotID] = list[i]
	}
	return out, nil
}

func (r *waitlistRepository) UpdateWithTx(tx *gorm.DB, e *model.WaitlistEntry) error {
	return tx.Save(e).Error
}
