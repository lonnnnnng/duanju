package app

type taskMutationSnapshot struct {
	tasks           map[string]*UITask
	values          map[string]UITask
	taskOrder       []string
	merges          map[string]*UIMergeState
	parsingInFlight map[string]bool
}

func (a *UIApp) snapshotTaskMutationLocked() taskMutationSnapshot {
	// long: 重试和继续会同时改任务、占位任务及解析标记；保存失败时必须整体恢复，避免接口报错却启动下载。
	snapshot := taskMutationSnapshot{
		tasks: make(map[string]*UITask, len(a.tasks)), values: make(map[string]UITask, len(a.tasks)),
		taskOrder: append([]string(nil), a.taskOrder...), merges: make(map[string]*UIMergeState, len(a.merges)),
		parsingInFlight: make(map[string]bool, len(a.parsingInFlight)),
	}
	for id, task := range a.tasks {
		snapshot.tasks[id] = task
		if task != nil {
			snapshot.values[id] = *task
		}
	}
	for id, merge := range a.merges {
		snapshot.merges[id] = merge
	}
	for id, active := range a.parsingInFlight {
		snapshot.parsingInFlight[id] = active
	}
	return snapshot
}

func (a *UIApp) restoreTaskMutationLocked(snapshot taskMutationSnapshot) {
	for id, task := range snapshot.tasks {
		if task != nil {
			*task = snapshot.values[id]
		}
	}
	a.tasks = snapshot.tasks
	a.taskOrder = snapshot.taskOrder
	a.merges = snapshot.merges
	a.parsingInFlight = snapshot.parsingInFlight
}
