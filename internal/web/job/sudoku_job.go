package job

import (
	"context"

	"github.com/SawaMEN/3x-ui/v3/internal/logger"
	"github.com/SawaMEN/3x-ui/v3/internal/sudoku"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
)

type SudokuJob struct {
	ctx context.Context
}

func NewSudokuJob(ctx context.Context) *SudokuJob {
	job := &SudokuJob{ctx: ctx}
	if ctx != nil {
		go func() {
			<-ctx.Done()
			sudoku.GetManager().StopAll()
		}()
	}
	return job
}

func (j *SudokuJob) Run() {
	desired, err := service.DesiredSudokuInstances()
	if err != nil {
		logger.Warning("sudoku job: build desired instances failed:", err)
		return
	}
	sudoku.GetManager().Reconcile(j.ctx, desired)
}
