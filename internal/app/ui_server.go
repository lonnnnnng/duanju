package app

import (
	"context"
	"crypto/sha256"
	"duanju/internal/diskspace"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	uiStatusQueued   = "queued"
	uiStatusParsing  = "parsing"
	uiStatusRunning  = "running"
	uiStatusSuccess  = "success"
	uiStatusFailed   = "failed"
	uiStatusCanceled = "canceled"
	uiStatusPaused   = "paused"
)

type UIApp struct {
	downloader               *Downloader
	cfg                      Config
	mu                       sync.Mutex
	dramas                   []Drama
	selected                 map[string]bool
	tasks                    map[string]*UITask
	taskOrder                []string
	merges                   map[string]*UIMergeState
	autoMerges               map[string]*UIAutoMergeState
	autoMergeCancel          context.CancelFunc
	autoMergeWG              sync.WaitGroup
	lastError                string
	loadedAt                 time.Time
	libraryLoading           chan struct{}
	librarySaved             bool
	libraryDirty             bool
	libraryAttempted         bool
	libraryRevision          uint64
	libraryLoadingGeneration uint64
	librarySources           map[string]librarySourceState
	libraryError             string
	libraryCancel            context.CancelFunc
	libraryWriteMu           sync.Mutex
	libraryLastSave          time.Time
	libraryApp               *hongguoCatalogState
	libraryMore              bool
	libraryLoadingSource     string
	libraryMetadata          libraryMetadataProgress
	metadataQueue            []string
	metadataPending          map[string]bool
	metadataCancel           context.CancelFunc
	metadataDone             chan struct{}
	metadataClosed           bool
	rankingApplied           map[string]time.Time

	cond                  *sync.Cond
	statePath             string
	address               string
	runningCancels        map[string]context.CancelFunc
	mergeMu               sync.Mutex
	mergeJobs             map[string]*uiMergeJob
	mergeStopped          bool
	parsingInFlight       map[string]bool
	parsingCancels        map[string]context.CancelFunc
	closed                bool
	workerCount           int
	workerWG              sync.WaitGroup
	parsingWG             sync.WaitGroup
	activeDownloads       int
	settingsMu            sync.Mutex
	nextOutputDir         string
	directoryPickerMu     sync.Mutex
	directoryPicker       func(context.Context, string) (string, error)
	playbackMu            sync.Mutex
	playbacks             map[string]*playbackSession
	playbackPrefetchSlots chan struct{}
	playbackResourcesOnce sync.Once
	playbackResources     *playbackResources
	viewersOnce           sync.Once
	viewers               *viewerManager
	embyMu                sync.Mutex
	updateMu              sync.Mutex
	embyKey               []byte
	embySyncOnce          sync.Once
	embySync              *embySyncManager
	dramaRefreshes        map[string]*dramaRefreshCall
	dramaRefreshSlots     chan struct{}
	coverImages           coverImageCache
	coverRepairs          map[string]*coverRepairCall
	coverRepairSlots      chan struct{}
}
type uiState struct {
	Dramas      []Drama                      `json:"dramas,omitempty"`
	LoadedAt    time.Time                    `json:"loadedAt,omitempty"`
	LastError   string                       `json:"lastError,omitempty"`
	TaskOrder   []string                     `json:"taskOrder"`
	Tasks       []*UITask                    `json:"tasks"`
	MergeStates map[string]*UIMergeState     `json:"merges,omitempty"`
	AutoMerges  map[string]*UIAutoMergeState `json:"autoMerges,omitempty"`
}

type UIMergeState struct {
	Automatic       bool      `json:"automatic,omitempty"`
	ResumeAutomatic bool      `json:"resumeAutomatic,omitempty"`
	WaitingForSpace bool      `json:"waitingForSpace,omitempty"`
	PlaybackTaskID  string    `json:"playbackTaskId,omitempty"`
	DeleteRequested bool      `json:"deleteRequested,omitempty"`
	DramaID         string    `json:"dramaId"`
	DramaTitle      string    `json:"dramaTitle,omitempty"`
	Status          string    `json:"status"`
	Progress        int       `json:"progress"`
	Merged          int       `json:"merged"`
	StartEpisode    int       `json:"startEpisode,omitempty"`
	EndEpisode      int       `json:"endEpisode,omitempty"`
	Total           int       `json:"total"`
	OutputPath      string    `json:"outputPath,omitempty"`
	Detail          string    `json:"detail,omitempty"`
	Error           string    `json:"error,omitempty"`
	Skipped         bool      `json:"skipped,omitempty"`
	DeleteEpisodes  bool      `json:"deleteEpisodes,omitempty"`
	CleanupPending  []string  `json:"cleanupPending,omitempty"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type uiMergeResult struct {
	ResumeAutomatic bool     `json:"-"`
	WaitingForSpace bool     `json:"-"`
	DramaID         string   `json:"dramaId"`
	DramaTitle      string   `json:"dramaTitle,omitempty"`
	OK              bool     `json:"ok"`
	PersistFailed   bool     `json:"-"`
	Skipped         bool     `json:"skipped,omitempty"`
	Merged          int      `json:"merged"`
	StartEpisode    int      `json:"startEpisode,omitempty"`
	EndEpisode      int      `json:"endEpisode,omitempty"`
	OutputPath      string   `json:"outputPath,omitempty"`
	Detail          string   `json:"detail,omitempty"`
	Error           string   `json:"error,omitempty"`
	CleanupPending  []string `json:"cleanupPending,omitempty"`
}

type UITask struct {
	NextSpaceCheck       time.Time `json:"nextSpaceCheck,omitempty"`
	ID                   string    `json:"id"`
	DramaID              string    `json:"dramaId"`
	DramaTitle           string    `json:"dramaTitle"`
	Episode              string    `json:"episode"`
	Title                string    `json:"title"`
	Status               string    `json:"status"`
	Progress             int       `json:"progress"`
	Error                string    `json:"error,omitempty"`
	Path                 string    `json:"path"`
	Attempt              int       `json:"attempt"`
	DownloadedBytes      int64     `json:"downloadedBytes"`
	TotalBytes           int64     `json:"totalBytes"`
	SpeedBytesPerSecond  float64   `json:"speedBytesPerSecond"`
	ElapsedSeconds       int64     `json:"elapsedSeconds"`
	RemainingSeconds     int64     `json:"remainingSeconds"`
	MediaElapsedSeconds  int64     `json:"mediaElapsedSeconds"`
	MediaTotalSeconds    int64     `json:"mediaTotalSeconds"`
	Phase                string    `json:"phase"`
	CreatedAt            time.Time `json:"createdAt"`
	UpdatedAt            time.Time `json:"updatedAt"`
	CancelRequested      bool      `json:"cancelRequested,omitempty"`
	RemoveRequested      bool      `json:"removeRequested,omitempty"`
	DeleteFilesRequested bool      `json:"deleteFilesRequested,omitempty"`
	PauseRequested       bool      `json:"pauseRequested,omitempty"`
	Source               Task      `json:"task"`
}

type uiTaskView struct {
	DownloadQuality      int       `json:"downloadQuality"`
	ID                   string    `json:"id"`
	DramaID              string    `json:"dramaId"`
	DramaTitle           string    `json:"dramaTitle"`
	Episode              string    `json:"episode"`
	Title                string    `json:"title"`
	Index                int       `json:"index"`
	Total                int       `json:"total"`
	Status               string    `json:"status"`
	Progress             int       `json:"progress"`
	Error                string    `json:"error,omitempty"`
	Path                 string    `json:"path"`
	Attempt              int       `json:"attempt"`
	DownloadedBytes      int64     `json:"downloadedBytes"`
	TotalBytes           int64     `json:"totalBytes"`
	SpeedBytesPerSecond  float64   `json:"speedBytesPerSecond"`
	ElapsedSeconds       int64     `json:"elapsedSeconds"`
	RemainingSeconds     int64     `json:"remainingSeconds"`
	MediaElapsedSeconds  int64     `json:"mediaElapsedSeconds"`
	MediaTotalSeconds    int64     `json:"mediaTotalSeconds"`
	Phase                string    `json:"phase"`
	CreatedAt            time.Time `json:"createdAt"`
	UpdatedAt            time.Time `json:"updatedAt"`
	CancelRequested      bool      `json:"cancelRequested,omitempty"`
	RemoveRequested      bool      `json:"removeRequested,omitempty"`
	DeleteFilesRequested bool      `json:"deleteFilesRequested,omitempty"`
	PauseRequested       bool      `json:"pauseRequested,omitempty"`
	ReleaseStatus        string    `json:"releaseStatus,omitempty"`
	Playable             bool      `json:"playable"`
	LocalFileUnavailable bool      `json:"localFileUnavailable,omitempty"`
	LocalFileRetryable   bool      `json:"localFileRetryable,omitempty"`
}

func NewUIApp(d *Downloader) *UIApp {
	cfg := d.cfg
	if cfg.OutputDir == "" {
		cfg.OutputDir = defaultOutputDir()
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 1
	}
	a := &UIApp{
		downloader:      d,
		cfg:             cfg,
		selected:        map[string]bool{},
		tasks:           map[string]*UITask{},
		merges:          map[string]*UIMergeState{},
		statePath:       filepath.Join(cfg.dataDirectory(), "ui-state.json"),
		runningCancels:  map[string]context.CancelFunc{},
		parsingInFlight: map[string]bool{},
		parsingCancels:  map[string]context.CancelFunc{},
		nextOutputDir:   cfg.outputDirSetting,
	}
	a.cond = sync.NewCond(&a.mu)
	a.loadState()
	a.loadLibrary()
	a.startWorkers()
	a.mu.Lock()
	a.restoreWaitingMergesLocked()
	a.mu.Unlock()
	return a
}

func (a *UIApp) restoreWaitingMergesLocked() {
	for id, state := range a.merges {
		if state != nil && state.WaitingForSpace && state.Status == "queued" {
			if state.Automatic {
				automatic := a.autoMerges[id]
				if automatic == nil || !automatic.Enabled {
					state.Status, state.WaitingForSpace, state.ResumeAutomatic, state.Error = "failed", false, false, "自动合并已关闭，原分集已保留"
					_ = a.saveStateLockedWithDiagnostic("ui.auto_merge_restore_disabled_save_failed")
					continue
				}
				// long: 自动合并的恢复也只选取原完整集合；缺集时退回等待，不能使用本剧剩下的成功分集生成局部全集。
				if tasks, reason := a.autoMergeTasksLocked(id, automatic); reason != "" {
					automatic.SubmittedGeneration, automatic.Status, automatic.Error = 0, "waiting", reason
					continue
				} else if jobs, err := a.enqueueMergesLocked([]string{id}, false, true); err != nil {
					state.Error = "恢复自动合并失败：" + a.redactError(err)
				} else {
					jobs[0].tasks, jobs[0].autoGeneration = tasks, automatic.Generation
					automatic.SubmittedGeneration = automatic.Generation
				}
				continue
			}
			if _, err := a.enqueueMergesLocked([]string{id}, state.DeleteEpisodes); err != nil {
				state.Error = "恢复空间等待任务失败：" + a.redactError(err)
			}
		}
	}
}

func (a *UIApp) ListenAndServe(addr string) error {
	return a.ListenAndServeContext(context.Background(), addr)
}

// long: 通过可取消上下文统一关闭 HTTP 服务和后台任务，避免进程退出时留下下载、解析或剧库更新 goroutine。
func (a *UIApp) ListenAndServeContext(ctx context.Context, addr string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	a.downloader.recordDiagnostic(diagnosticEvent{Level: "info", Event: "app.ui_starting", Message: "listen=" + addr})
	defer a.stopWorkers()
	if err := a.prepareBrowserViewers(); err != nil {
		return err
	}
	defer a.stopSortMetadata()
	a.embySyncer().start()
	defer a.embySyncer().stop()
	installer := a.downloader.ffmpegInstallation()
	defer installer.stop()
	go func() {
		path, err := installer.ensure(context.Background())
		if err != nil {
			fmt.Printf("FFmpeg 准备失败：%v\n", publicError(err))
		} else {
			fmt.Printf("FFmpeg 已就绪：%s\n", path)
		}
	}()
	a.mu.Lock()
	a.address = addr
	if err := a.saveStateLockedWithDiagnostic("ui.state_startup_save_failed"); err != nil {
		a.mu.Unlock()
		return fmt.Errorf("保存 UI 状态失败：%w", err)
	}
	a.mu.Unlock()

	defer a.stopMergeJobs()
	a.startAutoMerge()
	defer a.closePlaybacks()

	server := &http.Server{Addr: addr, Handler: a.routes(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		a.downloader.recordDiagnostic(diagnosticEvent{Event: "app.listen_failed", Message: a.redactError(err)})
		return err
	}
	a.downloader.recordDiagnostic(diagnosticEvent{Level: "info", Event: "app.listening", Message: "listen=" + addr})
	if err := markPendingUpdateReady(a.cfg.dataDirectory()); err != nil {
		a.downloader.recordDiagnostic(diagnosticEvent{Event: "app.update_ready_failed", Message: a.redactError(err)})
		_ = listener.Close()
		return err
	}
	serverDone := make(chan struct{})
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
			a.downloader.recordDiagnostic(diagnosticEvent{Level: "info", Event: "app.shutdown_requested", Message: "context canceled"})
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdownCtx)
		case <-serverDone:
		}
	}()
	err = server.Serve(listener)
	close(serverDone)
	// long: ListenAndServe 在收到 Shutdown 时会先于活动请求返回；等请求收尾后再停止解析和下载协程。
	<-shutdownDone
	level := "info"
	message := "ui service stopped"
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		level = "error"
		message = a.redactError(err)
	}
	a.downloader.recordDiagnostic(diagnosticEvent{Level: level, Event: "app.stopped", Message: message})
	return err
}

func (a *UIApp) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleIndex)
	mux.HandleFunc("/healthz", a.handleHealthz)
	mux.Handle("/assets/", webAssets())
	mux.HandleFunc("/api/ui/viewer", a.handleViewer)
	mux.HandleFunc("/api/ui/viewer/legacy", a.handleViewerLegacy)
	mux.HandleFunc("/api/ui/settings/update", a.handleAppUpdate)
	mux.HandleFunc("/api/ui/dramas", a.handleDramas)
	mux.HandleFunc("/api/ui/cover/repair", a.handleCoverRepair)
	mux.HandleFunc("/api/ui/dramas/refresh", a.handleDramaRefresh)
	mux.HandleFunc("/api/ui/search", a.handleLibrarySearch)
	mux.HandleFunc("/api/ui/following", a.handleFollowing)
	mux.HandleFunc("/api/ui/rankings", a.handleRankings)
	mux.HandleFunc("/api/ui/recommendations", a.handleRecommendations)
	mux.HandleFunc("/api/ui/download", a.handleDownload)
	mux.HandleFunc("/api/ui/tasks", a.handleTasks)
	mux.HandleFunc("/api/ui/update", a.handleUpdate)
	mux.HandleFunc("/api/ui/tasks/retry", a.handleRetry)
	mux.HandleFunc("/api/ui/tasks/cancel", a.handleCancel)
	mux.HandleFunc("/api/ui/tasks/pause", a.handlePause)
	mux.HandleFunc("/api/ui/tasks/resume", a.handleResume)
	mux.HandleFunc("/api/ui/tasks/clear", a.handleClear)
	mux.HandleFunc("/api/ui/tasks/delete", a.handleDeleteTasks)
	mux.HandleFunc("/api/ui/tasks/priority", a.handleTaskPriority)
	mux.HandleFunc("/api/ui/tasks/verify", a.handleVerifyTasks)
	mux.HandleFunc("/api/ui/merge", a.handleMerge)
	mux.HandleFunc("/api/ui/merge/auto", a.handleAutoMerge)
	mux.HandleFunc("/api/ui/merge/cancel", a.handleMergeCancel)
	mux.HandleFunc("/api/ui/merge/cleanup", a.handleMergeCleanup)
	mux.HandleFunc("/api/ui/ffmpeg", a.handleFFmpeg)
	mux.HandleFunc("/api/ui/config", a.handleConfig)
	mux.HandleFunc("/api/ui/network/check", a.handleNetworkCheck)
	mux.HandleFunc("/api/ui/directory/pick", a.handleDirectoryPicker)
	mux.HandleFunc("/api/ui/image", a.handleImage)
	mux.HandleFunc("/api/ui/settings/emby", a.handleEmbySyncSettings)
	mux.HandleFunc("/api/ui/settings/emby/sync", a.handleEmbySyncNow)
	mux.HandleFunc("/api/emby/export", a.handleEmbyExport)
	mux.HandleFunc("/api/emby/cover", a.handleEmbyCover)
	mux.HandleFunc("/api/emby/stream.m3u8", a.handleEmbyStream)
	mux.HandleFunc("/api/emby/merged.mp4", a.handleEmbyMerged)
	mux.HandleFunc("/api/emby/segment.ts", a.handleEmbySegment)
	a.registerPlaybackRoutes(mux)
	return a.withServiceAccess(a.withBrowserViewer(mux))
}

func (a *UIApp) startWorkers() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ensureWorkersLocked()
}

func (a *UIApp) ensureWorkersLocked() {
	for a.workerCount < a.cfg.Concurrency {
		a.workerCount++
		a.workerWG.Add(1)
		go func() {
			defer a.workerWG.Done()
			a.worker()
		}()
	}
}

// long: 先取消所有后台上下文，再唤醒等待队列并等待 goroutine 退出，确保状态文件不会在服务返回后继续被写入。
func (a *UIApp) stopWorkers() {
	a.mu.Lock()
	a.closed = true
	if a.autoMergeCancel != nil {
		a.autoMergeCancel()
	}
	cancels := make([]context.CancelFunc, 0, len(a.runningCancels)+len(a.parsingCancels)+1)
	if a.libraryCancel != nil {
		cancels = append(cancels, a.libraryCancel)
	}
	for _, cancel := range a.runningCancels {
		if cancel != nil {
			cancels = append(cancels, cancel)
		}
	}
	for _, cancel := range a.parsingCancels {
		if cancel != nil {
			cancels = append(cancels, cancel)
		}
	}
	libraryDone := a.libraryLoading
	if a.cond != nil {
		a.cond.Broadcast()
	}
	a.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
	if libraryDone != nil {
		<-libraryDone
	}
	a.workerWG.Wait()
	a.parsingWG.Wait()
	a.autoMergeWG.Wait()
}

func (a *UIApp) worker() {
	for {
		a.mu.Lock()
		var task *UITask
		for task == nil && !a.closed {
			var retryDelay time.Duration
			if a.activeDownloads >= a.cfg.Concurrency {
				a.cond.Wait()
				continue
			}
			for _, id := range a.taskOrder {
				candidate := a.tasks[id]
				if candidate != nil && candidate.Status == uiStatusQueued {
					if delay := time.Until(candidate.NextSpaceCheck); delay > 0 {
						if retryDelay == 0 || delay < retryDelay {
							retryDelay = delay
						}
						continue
					}
					task = candidate
					break
				}
			}
			if task == nil && !a.closed {
				if retryDelay > 0 {
					timer := time.AfterFunc(retryDelay, func() {
						// long: 定时唤醒与队列检查使用同一把锁，防止通知早于 Wait 而让空间等待任务永久休眠。
						a.mu.Lock()
						a.cond.Broadcast()
						a.mu.Unlock()
					})
					a.cond.Wait()
					timer.Stop()
				} else {
					a.cond.Wait()
				}
			}
		}
		if a.closed {
			a.mu.Unlock()
			return
		}

		if info, err := os.Stat(task.Source.OutPath); err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			if err := a.downloader.requireSpace(task.Source.OutPath, 0); err != nil {
				task.Phase, task.Error, task.NextSpaceCheck = "waiting-space", err.Error(), time.Now().Add(30*time.Second)
				_ = a.saveStateLockedWithDiagnostic("ui.space_wait_save_failed")
				a.mu.Unlock()
				continue
			}
		}
		now := time.Now()
		before := *task
		a.activeDownloads++
		task.Status = uiStatusRunning
		task.Progress = 0
		task.Error = ""
		task.CancelRequested = false
		task.PauseRequested = false
		task.SpeedBytesPerSecond = 0
		task.DownloadedBytes = 0
		task.ElapsedSeconds = 0
		task.MediaElapsedSeconds = 0
		task.RemainingSeconds = 0
		task.Phase = "starting"
		task.NextSpaceCheck = time.Time{}
		task.Attempt++
		task.UpdatedAt = now
		taskID := task.ID
		downloadTask := task.Source
		ctx, cancel := context.WithTimeout(context.Background(), uiTaskTimeout(downloadTask))
		a.runningCancels[taskID] = cancel
		if err := a.saveStateLockedWithDiagnostic("ui.task_start_save_failed"); err != nil {
			// long: 下载开始前必须先保存 running 状态；保存失败时恢复队列并稍后重试，避免写了视频却丢失任务进度。
			*task = before
			a.activeDownloads--
			delete(a.runningCancels, taskID)
			a.mu.Unlock()
			cancel()
			time.Sleep(time.Second)
			continue
		}
		a.mu.Unlock()

		err := a.downloader.DownloadEpisodeWithProgress(ctx, downloadTask, func(p DownloadProgress) {
			a.mu.Lock()
			current := a.tasks[taskID]
			if current != nil && current.Status == uiStatusRunning {
				current.Progress = p.Percent
				current.DownloadedBytes = p.DownloadedBytes
				current.TotalBytes = p.TotalBytes
				current.SpeedBytesPerSecond = p.SpeedBytesPerSecond
				current.ElapsedSeconds = int64(p.Elapsed / time.Second)
				current.RemainingSeconds = int64(p.Remaining / time.Second)
				current.MediaElapsedSeconds = int64(p.MediaElapsed / time.Second)
				current.MediaTotalSeconds = int64(p.MediaTotal / time.Second)
				current.Phase = p.Phase
				current.UpdatedAt = time.Now()
			}
			a.mu.Unlock()
		})
		cancel()

		a.mu.Lock()
		a.activeDownloads--
		delete(a.runningCancels, taskID)
		current := a.tasks[taskID]
		if current != nil && current.Status == uiStatusRunning {
			current.UpdatedAt = time.Now()
			current.RemainingSeconds = 0
			current.SpeedBytesPerSecond = 0
			if current.PauseRequested && !current.RemoveRequested {
				current.Status = uiStatusPaused
				// long: 下载的临时文件已在协程退出时清理，暂停后清零写入量和进度，避免误显示可续传的旧数据。
				current.Progress = 0
				current.DownloadedBytes = 0
				current.ElapsedSeconds = 0
				current.MediaElapsedSeconds = 0
				current.PauseRequested = false
				current.CancelRequested = false
				current.Error = ""
				current.Phase = "paused"
			} else if current.CancelRequested || errors.Is(err, context.Canceled) {
				current.Status = uiStatusCanceled
				current.Progress = 0
				current.Error = "已取消"
				current.Phase = "canceled"
			} else if diskspace.IsWaiting(err) {
				current.Status, current.Phase, current.Error = uiStatusQueued, "waiting-space", err.Error()
				current.Progress, current.DownloadedBytes = 0, 0
				current.NextSpaceCheck = time.Now().Add(30 * time.Second)
				if current.Attempt > 0 {
					current.Attempt--
				}
			} else if err != nil {
				current.Status = uiStatusFailed
				current.Progress = 0
				if errors.Is(err, context.DeadlineExceeded) {
					current.Error = "下载任务超时，已自动失败，可稍后重试"
				} else {
					current.Error = a.redactError(err)
				}
				current.Phase = "failed"
			} else {
				current.Status = uiStatusSuccess
				current.Progress = 100
				current.Error = ""
				current.Phase = "completed"
				if st, statErr := os.Stat(current.Path); statErr == nil && !st.IsDir() {
					current.DownloadedBytes = st.Size()
					if current.TotalBytes <= 0 {
						current.TotalBytes = st.Size()
					}
				}
			}
			if current.RemoveRequested {
				if current.DeleteFilesRequested {
					// long: 下载协程退出后才删最终文件，防止取消与重命名竞争造成视频残留。
					if _, deleteErr := a.deleteTaskFiles(current); deleteErr != nil {
						current.RemoveRequested, current.DeleteFilesRequested = false, false
						current.Status, current.Phase = uiStatusFailed, "failed"
						current.Error = "删除本地文件失败：" + a.redactError(deleteErr)
					} else {
						pathDir := filepath.Dir(firstNonEmpty(current.Path, current.Source.OutPath))
						dramaID := current.DramaID
						a.removeTaskLocked(taskID)
						// long: 下载 worker 退出后才可能删掉最后一个任务；此时检查标记目录是否也能安全回收。
						a.cleanupDownloadDirectoryLocked(pathDir, dramaID)
					}
				} else {
					a.removeTaskLocked(taskID)
				}
			}
			if current != nil && a.tasks[taskID] == current && current.Status == uiStatusSuccess {
				a.autoMergeDownloadFinishedLocked(current)
			}
			saveErr := a.saveStateLockedWithDiagnostic("ui.task_finish_save_failed")
			a.mu.Unlock()
			a.cond.Broadcast()
			if saveErr != nil {
				saveErr = a.retryTaskFinishSave()
			}
			if err == nil && saveErr == nil {
				if current != nil && current.Status == uiStatusSuccess {
					a.downloader.recordDiagnostic(diagnosticEvent{Level: "info", Event: "download.completed",
						Source: firstNonEmpty(downloadTask.Chapter.Source, sourceFromDramaID(downloadTask.DramaID)), DramaID: downloadTask.DramaID,
						DramaTitle: downloadTask.DramaTitle, Episode: downloadTask.Index, Attempt: current.Attempt,
						Quality: downloadTask.DownloadQuality, TotalBytes: current.DownloadedBytes, Message: "download completed"})
				}
				a.notifyEmbySync()
			}
			continue
		}
		a.mu.Unlock()
		a.cond.Broadcast()
	}
}

func (a *UIApp) retryTaskFinishSave() error {
	// long: 视频写入与状态文件无法原子提交；短暂磁盘故障可在后台补写完成状态，避免重启把旧 running 任务重新排队。
	var saveErr error
	for attempt := 1; attempt <= 3; attempt++ {
		time.Sleep(time.Duration(attempt) * 250 * time.Millisecond)
		a.mu.Lock()
		saveErr = a.saveStateLockedWithDiagnostic("ui.task_finish_retry_save_failed")
		a.mu.Unlock()
		if saveErr == nil {
			return nil
		}
	}
	return saveErr
}

func (a *UIApp) migrateTaskPathLocked(task *UITask) bool {
	if task == nil {
		return false
	}
	if task.Path != "" || task.Source.OutPath != "" {
		return false
	}
	index := task.Source.Index
	if index <= 0 {
		index = episodeNumber(task.Episode)
	}
	if index <= 0 {
		return false
	}
	title := strings.TrimSpace(task.DramaTitle)
	if title == "" {
		title = strings.TrimSpace(task.Source.DramaTitle)
	}
	if title == "" {
		return false
	}
	dramaID := strings.TrimSpace(task.DramaID)
	if dramaID == "" {
		dramaID = strings.TrimSpace(task.Source.DramaID)
	}
	dramaDir := filepath.Join(a.cfg.OutputDir, safeFilename(title))
	markerPath := filepath.Join(dramaDir, ".drama-id")
	if marker, err := os.ReadFile(markerPath); err == nil && strings.TrimSpace(string(marker)) != "" && dramaID != "" && strings.TrimSpace(string(marker)) != dramaID {
		dramaDir = filepath.Join(a.cfg.OutputDir, safeFilename(title+"_"+hashShort(dramaID)))
		markerPath = filepath.Join(dramaDir, ".drama-id")
	}
	if err := ensureSharedMediaDirectory(dramaDir); err != nil {
		return false
	}
	if dramaID != "" {
		_ = os.WriteFile(markerPath, []byte(dramaID), 0o644)
	}
	newPath := filepath.Join(dramaDir, fmt.Sprintf("%03d.mp4", index))
	task.Path = newPath
	task.Source.OutPath = newPath
	task.DramaID = dramaID
	task.Source.DramaID = dramaID
	task.DramaTitle = title
	task.Source.DramaTitle = title
	return true
}

func episodeNumber(value string) int {
	value = strings.TrimSpace(value)
	if n, err := strconv.Atoi(value); err == nil && n > 0 {
		return n
	}
	return 0
}

func isInvalidPersistedProviderTask(task *UITask) bool {
	if task == nil {
		return false
	}
	providerTask := canonicalProviderSource(task.Source.Chapter.Source) == sourceHongguo
	if !providerTask {
		_, _, providerTask = splitProviderDramaID(task.DramaID)
	}
	if !providerTask {
		_, _, providerTask = splitProviderDramaID(task.Source.DramaID)
	}
	if !providerTask {
		return false
	}
	total := task.Source.Total
	if total <= 0 || total > 300 {
		return false
	}
	ep := episodeNumber(task.Episode)
	if ep <= 0 {
		ep = episodeNumber(task.Source.Chapter.EpisodeString(task.Source.Index))
	}
	return ep > total
}

func (a *UIApp) loadState() {
	b, err := os.ReadFile(a.statePath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			a.lastError = a.redactString(err.Error())
		}
		return
	}
	var state uiState
	if err := json.Unmarshal(b, &state); err != nil {
		a.lastError = a.redactString(err.Error())
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.dramas = onlyHongguoDramas(state.Dramas)
	a.normalizeDramaCovers(a.dramas)
	a.loadedAt = state.LoadedAt
	a.lastError = a.redactString(state.LastError)
	if a.merges == nil {
		a.merges = make(map[string]*UIMergeState)
	}
	a.autoMerges = cloneAutoMergeStates(state.AutoMerges)
	for id := range a.autoMerges {
		if sourceFromDramaID(id) != sourceHongguo {
			delete(a.autoMerges, id)
		}
	}
	for identifier, merge := range state.MergeStates {
		if sourceFromDramaID(identifier) == sourceHongguo {
			a.merges[identifier] = merge
		}
	}
	seen := map[string]bool{}
	changed := len(a.dramas) != len(state.Dramas)
	// long: 重启恢复时尚未装载完其他任务，延后回收目录，避免误删仍被排队任务引用的空目录。
	cleanupDirs := map[string]string{}
	for id, state := range a.merges {
		if state != nil && state.DeleteRequested {
			// long: 删除意图已落盘时，即使上次进程在删文件与保存结果之间退出，也继续完成全集删除。
			if state.OutputPath != "" {
				if _, deleteErr := a.deleteManagedPath(state.OutputPath, id); deleteErr != nil {
					state.Error = "删除本地全集失败：" + a.redactError(deleteErr)
					changed = true
					continue
				}
			}
			delete(a.merges, id)
			if state.OutputPath != "" {
				cleanupDirs[filepath.Dir(state.OutputPath)] = id
			}
			changed = true
			continue
		}
		if state != nil && (state.Status == "running" || state.Status == "queued") && !state.WaitingForSpace {
			state.Status, state.Detail, state.Error = "failed", "", "服务已重启，合并已中断；原分集已保留，可重新合并"
			if automatic := a.autoMerges[id]; automatic != nil && automatic.Enabled {
				automatic.SubmittedGeneration, automatic.Status = 0, "waiting"
			}
			changed = true
		}
	}
	for _, task := range state.Tasks {
		if task == nil || sourceFromDramaID(task.DramaID) != sourceHongguo || !isHongguoTask(task.Source) {
			changed = true
			continue
		}
		if task.RemoveRequested {
			if task.DeleteFilesRequested {
				// long: 进程重启前已确认的删除继续收尾，避免任务消失而磁盘资源残留。
				if _, deleteErr := a.deleteTaskFiles(task); deleteErr != nil {
					task.RemoveRequested, task.DeleteFilesRequested = false, false
					task.Status, task.Phase = uiStatusFailed, "failed"
					task.Error = "删除本地文件失败：" + a.redactError(deleteErr)
				} else {
					pathDir := filepath.Dir(firstNonEmpty(task.Path, task.Source.OutPath))
					changed = true
					cleanupDirs[pathDir] = task.DramaID
					continue
				}
			} else {
				changed = true
				continue
			}
			changed = true
		}
		if task.PauseRequested {
			task.Status = uiStatusPaused
			// long: 重启前已确认的暂停仍需按临时文件被清理后的状态展示，不能保留上次下载进度。
			task.Progress = 0
			task.DownloadedBytes = 0
			task.ElapsedSeconds = 0
			task.MediaElapsedSeconds = 0
			task.SpeedBytesPerSecond = 0
			task.RemainingSeconds = 0
			task.Phase = "paused"
			task.PauseRequested = false
			task.CancelRequested = false
			changed = true
		}
		if task.CancelRequested && (task.Status == uiStatusRunning || task.Status == uiStatusParsing || task.Status == "") {
			// long: 用户取消下载的意图已经落盘；重启时直接收尾为已取消，不能把中断的任务重新加入下载队列。
			task.Status = uiStatusCanceled
			task.Progress = 0
			task.Error = "已取消"
			task.CancelRequested = false
			task.SpeedBytesPerSecond = 0
			task.RemainingSeconds = 0
			task.Phase = "canceled"
			task.UpdatedAt = time.Now()
			changed = true
		} else if task.Status == uiStatusRunning || task.Status == "" {
			task.Status = uiStatusQueued
			task.Progress = 0
			task.CancelRequested = false
			task.SpeedBytesPerSecond = 0
			task.RemainingSeconds = 0
			task.Phase = "queued"
			task.UpdatedAt = time.Now()
			changed = true
		}
		if task.Status == uiStatusParsing || task.Phase == "parsing" {
			task.Status = uiStatusFailed
			task.Title = "章节获取失败"
			task.Progress = 0
			task.Error = "上次章节解析中断，请点击重试重新获取章节"
			task.Phase = "failed"
			source := firstNonEmpty(task.Source.Chapter.Source, sourceFromDramaID(task.DramaID), sourceFromDramaID(task.Source.DramaID), "unknown")
			sourceID := firstNonEmpty(task.Source.DramaID, task.DramaID)
			if _, parsedID, ok := splitProviderDramaID(sourceID); ok {
				sourceID = parsedID
			}
			task.Source.Chapter.Source = source
			task.Source.Chapter.ID = providerChapterID(source, sourceID, "fetch-failed")
			task.Source.Chapter.Title = "章节处理中"
			task.UpdatedAt = time.Now()
			changed = true
		}
		if task.Path == "" {
			task.Path = task.Source.OutPath
			changed = true
		}
		if task.Source.OutPath == "" {
			task.Source.OutPath = task.Path
			changed = true
		}
		if isInvalidPersistedProviderTask(task) {
			changed = true
			continue
		}
		if !isFetchFailedPlaceholderTask(task) {
			if a.migrateTaskPathLocked(task) {
				changed = true
			}
		}
		if newID := uiTaskID(task.Path); newID != "" && task.ID != newID {
			task.ID = newID
			changed = true
		}
		if task.Status == uiStatusRunning || task.Status == "" {
			task.Status = uiStatusQueued
			task.Progress = 0
			task.CancelRequested = false
			task.SpeedBytesPerSecond = 0
			task.RemainingSeconds = 0
			task.Phase = "queued"
			task.UpdatedAt = time.Now()
			changed = true
		}
		task.Error = a.redactString(task.Error)
		if task.CreatedAt.IsZero() {
			task.CreatedAt = time.Now()
			changed = true
		}
		if task.UpdatedAt.IsZero() {
			task.UpdatedAt = task.CreatedAt
			changed = true
		}
		if task.ID != "" && !seen[task.ID] {
			seen[task.ID] = true
			a.tasks[task.ID] = task
		}
	}
	for _, id := range state.TaskOrder {
		if a.tasks[id] != nil && seen[id] {
			a.taskOrder = append(a.taskOrder, id)
			seen[id] = false
		}
	}
	ids := make([]string, 0, len(a.tasks))
	for id := range a.tasks {
		if seen[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	a.taskOrder = append(a.taskOrder, ids...)
	for directory, dramaID := range cleanupDirs {
		a.cleanupDownloadDirectoryLocked(directory, dramaID)
	}
	if changed {
		_ = a.saveStateLockedWithDiagnostic("ui.state_migration_save_failed")
	}
}

func (a *UIApp) saveStateLocked() error {
	directory := filepath.Dir(a.statePath)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		a.lastError = a.redactString(err.Error())
		return err
	}
	state := uiState{
		LoadedAt:    a.loadedAt,
		LastError:   a.lastError,
		TaskOrder:   append([]string(nil), a.taskOrder...),
		Tasks:       make([]*UITask, 0, len(a.tasks)),
		MergeStates: cloneMergeStates(a.merges),
		AutoMerges:  cloneAutoMergeStates(a.autoMerges),
	}
	if !a.librarySaved {
		state.Dramas = append([]Drama(nil), a.dramas...)
	}
	for _, id := range a.taskOrder {
		if task := a.tasks[id]; task != nil {
			state.Tasks = append(state.Tasks, task)
		}
	}
	tmp, err := os.CreateTemp(directory, ".ui-state-*.tmp")
	if err != nil {
		a.lastError = a.redactString(err.Error())
		return err
	}
	tmpName := tmp.Name()
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(state); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		a.lastError = a.redactString(err.Error())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		a.lastError = a.redactString(err.Error())
		return err
	}
	if err := os.Rename(tmpName, a.statePath); err != nil {
		_ = os.Remove(tmpName)
		a.lastError = a.redactString(err.Error())
		return err
	}
	return nil
}

func (a *UIApp) saveStateLockedWithDiagnostic(event string) error {
	err := a.saveStateLocked()
	if err != nil && a.downloader != nil {
		a.downloader.recordDiagnostic(diagnosticEvent{Event: event, Message: a.redactError(err)})
	}
	return err
}

func (a *UIApp) writeStatePersistenceError(w http.ResponseWriter, err error) {
	message := "任务状态保存失败，请检查数据目录权限"
	if err != nil {
		message += "：" + a.redactError(err)
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": message, "code": "state_persist_failed"})
}

func (a *UIApp) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(uiHTML))
}

func (a *UIApp) handleDramas(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	refresh := r.URL.Query().Get("refresh") == "1"
	more := r.URL.Query().Get("more") == "1"
	update := r.URL.Query().Get("update") == "1"
	if refresh && more || update && (refresh || more) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "更新操作不能同时指定多个模式"})
		return
	}
	source := r.URL.Query().Get("source")
	if source != "" && canonicalProviderSource(source) != sourceHongguo {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "不支持刷新该站源"})
		return
	}
	if source != "" {
		source = sourceHongguo
	}
	var priority []string
	if raw := r.URL.Query().Get("priority"); (update || more) && raw != "" {
		priority = strings.Split(raw, ",")
		if len(raw) > 6000 || len(priority) > sortMetadataBatchSize {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "优先补齐条目过多"})
			return
		}
		for index, id := range priority {
			_, sourceID, valid := splitProviderDramaID(id)
			if len(id) > 120 || !valid {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的剧集 ID"})
				return
			}
			priority[index] = providerDramaID(sourceHongguo, sourceID)
		}
	}
	a.mu.Lock()
	updateAccepted := update && a.libraryLoading == nil
	if a.libraryLoading == nil && (update || refresh || more || len(a.dramas) == 0 && !a.libraryAttempted) {
		mode := libraryLoadRefresh
		if update {
			mode = libraryLoadUpdate
		} else if more {
			mode = libraryLoadMore
		}
		a.startLibraryLoadLocked(source, mode, priority)
	}
	revision, _ := strconv.ParseUint(r.URL.Query().Get("revision"), 10, 64)
	resp := a.librarySnapshotLocked(revision)
	if update {
		resp["updateAccepted"] = updateAccepted
	}
	a.mu.Unlock()
	writeJSON(w, http.StatusOK, resp)
}

func (a *UIApp) normalizeDramaCovers(dramas []Drama) {
	for i := range dramas {
		normalizeDramaCover(&dramas[i])
	}
}

func normalizeDramaCover(drama *Drama) {
	drama.OnlineDate = normalizeDate(drama.OnlineDate)
	drama.Views = normalizeViews(drama.Views)
	if p := bestDramaCover(*drama); p != "" {
		drama.Cover = "/api/ui/image?url=" + url.QueryEscape(p)
	}
}

func bestDramaCover(dr Drama) string {
	for _, v := range []any{dr.CoverURL, dr.CoverURLSnake, dr.Cover, dr.ImageURL, dr.ImageURLSnake, dr.Image, dr.Img, dr.Pic, dr.Picture, dr.Poster, dr.Thumb, dr.Thumbnail} {
		if p := coverPathFromAny(v); p != "" {
			return p
		}
	}
	return ""
}

func coverPathFromAny(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return ""
		}
		if strings.HasPrefix(s, "/api/ui/image") {
			if u, err := url.Parse(s); err == nil {
				if raw := strings.TrimSpace(u.Query().Get("url")); raw != "" {
					return raw
				}
			}
		}
		if u, err := url.Parse(s); err == nil && u.IsAbs() {
			return u.String()
		}
		return strings.TrimLeft(s, "/")
	case map[string]any:
		for _, key := range []string{"url", "src", "path", "cover", "coverUrl", "cover_url", "image", "pic", "poster"} {
			if s := coverPathFromAny(x[key]); s != "" {
				return s
			}
		}
	}
	return ""
}

func (a *UIApp) handleImage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	imgPath := strings.TrimSpace(r.URL.Query().Get("url"))
	if imgPath == "" {
		http.Error(w, "missing url", http.StatusBadRequest)
		return
	}
	remoteURL, ok := buildImageURL(imgPath)
	if !ok {
		http.Error(w, "invalid image url", http.StatusBadRequest)
		return
	}
	source, allowed := a.imageSource(r.Context(), remoteURL)
	if !allowed {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "封面不在当前剧库中"})
		return
	}
	ctx, cancel := context.WithTimeout(context.WithValue(r.Context(), coverSourceKey{}, source), 90*time.Second)
	defer cancel()
	buf, err := a.loadCoverImage(ctx, remoteURL, nil)
	if err != nil {
		if r.Context().Err() == nil {
			remote, _ := url.Parse(remoteURL)
			a.downloader.recordDiagnostic(diagnosticEvent{Event: "cover.failed", Host: remote.Hostname(), Message: a.redactError(err)})
		}
		http.Error(w, a.redactError(err), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", imageContentType(buf))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf)
}

func buildImageURL(imagePath string) (string, bool) {
	imagePath = strings.TrimSpace(imagePath)
	if imagePath == "" {
		return "", false
	}
	base, _ := url.Parse(hongguoBaseURL + "/")
	reference, err := url.Parse(imagePath)
	if err != nil || strings.Contains(imagePath, "..") {
		return "", false
	}
	resolved := base.ResolveReference(reference)
	if !validImageURL(resolved) {
		return "", false
	}
	return resolved.String(), true
}

func sourceCoverReferer(source, remoteURL string) string { return hongguoBaseURL + "/" }

func imageReferer(remoteURL string) string { return hongguoBaseURL + "/" }

func imageContentType(buf []byte) string {
	if len(buf) >= 12 && buf[0] == 'R' && buf[1] == 'I' && buf[2] == 'F' && buf[3] == 'F' && buf[8] == 'W' && buf[9] == 'E' && buf[10] == 'B' && buf[11] == 'P' {
		return "image/webp"
	}
	if len(buf) >= 8 && buf[0] == 0x89 && buf[1] == 0x50 && buf[2] == 0x4e && buf[3] == 0x47 {
		return "image/png"
	}
	if len(buf) >= 3 && buf[0] == 0x47 && buf[1] == 0x49 && buf[2] == 0x46 {
		return "image/gif"
	}
	if len(buf) >= 2 && buf[0] == 0xff && buf[1] == 0xd8 {
		return "image/jpeg"
	}
	return http.DetectContentType(buf)
}

func isKnownImage(buf []byte) bool {
	return imageContentType(buf) == "image/jpeg" || imageContentType(buf) == "image/png" || imageContentType(buf) == "image/gif" || imageContentType(buf) == "image/webp"
}

func (a *UIApp) handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	ids, quality, ok := readDownloadRequest(w, r)
	if !ok {
		return
	}
	views, saveErr := a.enqueueDramasAsync(ids, false, quality)
	if saveErr != nil {
		a.writeStatePersistenceError(w, saveErr)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"data": views, "pending": true})
}

func (a *UIApp) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	ids, quality, ok := readDownloadRequest(w, r)
	if !ok {
		return
	}
	views, saveErr := a.enqueueDramasAsync(ids, true, quality)
	if saveErr != nil {
		a.writeStatePersistenceError(w, saveErr)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"data": views, "pending": true})
}

func (a *UIApp) enqueueDramasAsync(ids []string, updateOnly bool, requestedQuality *int) ([]uiTaskView, error) {
	a.mu.Lock()
	// long: 下载请求只有在占位任务落盘后才算入队，保存失败时不能留下解析标记或启动后台解析。
	snapshot := a.snapshotTaskMutationLocked()
	if a.parsingInFlight == nil {
		a.parsingInFlight = map[string]bool{}
	}
	dramaByID := make(map[string]Drama, len(a.dramas))
	for _, dr := range a.dramas {
		if strings.TrimSpace(dr.ID) != "" {
			dramaByID[dr.ID] = dr
		}
	}
	views := make([]uiTaskView, 0, len(ids))
	dramas := make([]Drama, 0, len(ids))
	for _, id := range ids {
		_, sourceID, valid := splitProviderDramaID(id)
		if !valid {
			continue
		}
		id = providerDramaID(sourceHongguo, sourceID)
		drama, found := dramaByID[id]
		if !found {
			drama = Drama{ID: id, Title: "短剧"}
		}
		drama, valid = normalizeHongguoDrama(drama)
		if !valid {
			continue
		}
		dramaKey := placeholderDramaKey(drama)
		if a.parsingInFlight[dramaKey] {
			if view, ok := a.existingParsingPlaceholderViewLocked(drama); ok {
				views = append(views, view)
			}
			continue
		}
		quality := 0
		if updateOnly {
			for _, taskID := range a.taskOrder {
				if task := a.tasks[taskID]; task != nil && task.DramaID == drama.ID {
					quality = task.Source.DownloadQuality
					break
				}
			}
		}
		if requestedQuality != nil {
			quality = *requestedQuality
		}
		dramas = append(dramas, drama)
		a.parsingInFlight[dramaKey] = true
		if view, changed := a.addParsingPlaceholderTaskLocked(drama, quality); view.ID != "" {
			views = append(views, view)
			if changed {
				a.cond.Broadcast()
			}
		}
	}
	saveErr := a.saveStateLockedWithDiagnostic("ui.download_enqueue_save_failed")
	if saveErr != nil {
		a.restoreTaskMutationLocked(snapshot)
	}
	a.mu.Unlock()
	if saveErr != nil {
		return nil, saveErr
	}
	for _, drama := range dramas {
		a.startDramaParse(drama, updateOnly)
	}
	return views, saveErr
}

func placeholderDramaKey(drama Drama) string {
	if id := strings.TrimSpace(drama.ID); id != "" {
		return id
	}
	return strings.Join([]string{strings.TrimSpace(drama.Source), strings.TrimSpace(drama.SourceID), drama.DisplayTitle()}, "|")
}

func (a *UIApp) existingParsingPlaceholderViewLocked(drama Drama) (uiTaskView, bool) {
	task := a.placeholderTask(drama, "parsing")
	id := uiTaskID(task.OutPath)
	if existing := a.tasks[id]; existing != nil && existing.Status == uiStatusParsing && existing.Phase == "parsing" {
		return taskToView(existing), true
	}
	return uiTaskView{}, false
}

func (a *UIApp) markParsingFinished(dramas []Drama) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.parsingInFlight == nil {
		return
	}
	for _, drama := range dramas {
		delete(a.parsingInFlight, placeholderDramaKey(drama))
		delete(a.parsingCancels, placeholderDramaKey(drama))
	}
}

func (a *UIApp) enqueueDramas(ctx context.Context, dramas []Drama, updateOnly bool, quality int) ([]uiTaskView, []uiTaskView, string) {
	views := []uiTaskView{}
	failedViews := []uiTaskView{}
	respErr := ""
	for _, drama := range dramas {
		a.mu.Lock()
		existingDirectory := ""
		for _, id := range a.taskOrder {
			if task := a.tasks[id]; task != nil && task.DramaID == drama.ID && !isChapterPlaceholderTask(task) && task.Path != "" {
				existingDirectory = filepath.Dir(task.Path)
				break
			}
		}
		a.mu.Unlock()
		built, err := a.downloader.buildDramaTasksInDirectory(ctx, drama, existingDirectory)
		if err != nil {
			msg := fmt.Sprintf("《%s》加入失败：%s", drama.DisplayTitle(), a.redactError(err))
			if respErr != "" {
				respErr += "\n"
			}
			respErr += msg
			a.mu.Lock()
			if a.finishCanceledParseLocked(drama) {
				a.mu.Unlock()
				continue
			}
			a.lastError = respErr
			a.removeParsingPlaceholderTaskLocked(drama)
			if view, changed := a.addFailedPlaceholderTaskLocked(drama, err, quality); view.ID != "" {
				failedViews = append(failedViews, view)
				if changed {
					a.cond.Broadcast()
				}
			}
			_ = a.saveStateLockedWithDiagnostic("ui.parse_failure_save_failed")
			a.mu.Unlock()
			continue
		}
		if len(built) == 0 {
			msg := fmt.Sprintf("《%s》没有可下载章节", drama.DisplayTitle())
			if respErr != "" {
				respErr += "\n"
			}
			respErr += msg
			a.mu.Lock()
			if a.finishCanceledParseLocked(drama) {
				a.mu.Unlock()
				continue
			}
			a.lastError = respErr
			a.removeParsingPlaceholderTaskLocked(drama)
			if view, changed := a.addFailedPlaceholderTaskLocked(drama, errors.New("没有可下载章节"), quality); view.ID != "" {
				failedViews = append(failedViews, view)
				if changed {
					a.cond.Broadcast()
				}
			}
			_ = a.saveStateLockedWithDiagnostic("ui.parse_empty_save_failed")
			a.mu.Unlock()
			continue
		}

		a.mu.Lock()
		if a.finishCanceledParseLocked(drama) {
			a.mu.Unlock()
			continue
		}
		// long: 章节解析后的占位任务和真实分集必须一起落盘；失败时恢复占位，不能让 worker 下载未登记的分集。
		snapshot := a.snapshotTaskMutationLocked()
		viewStart := len(views)
		a.removeParsingPlaceholderTaskLocked(drama)
		a.removeFailedPlaceholderTaskLocked(drama)
		pathIndex := a.pathIndexLocked()
		now := time.Now()
		changed := true
		for _, task := range built {
			task.DownloadQuality = quality
			if existing := pathIndex[task.OutPath]; existing != nil {
				task.DownloadQuality = existing.Source.DownloadQuality
				if existing.Status != uiStatusRunning {
					existing.Source = task
				}
				if existing.Status == uiStatusSuccess {
					if fileOK, _ := existingGood(existing.Path, a.cfg.SkipBytes); fileOK {
						views = append(views, taskToView(existing))
						continue
					}
					if updateOnly {
						existing.Status = uiStatusQueued
						existing.Progress = 0
						existing.Error = ""
						existing.Phase = "queued"
						existing.DownloadedBytes = 0
						existing.TotalBytes = task.Chapter.MediaSize
						existing.UpdatedAt = now
						views = append(views, taskToView(existing))
						changed = true
						continue
					}
				}
				views = append(views, taskToView(existing))
				continue
			}
			uiTask := newUITask(task, now)
			if ok, size := existingGood(task.OutPath, a.cfg.SkipBytes); ok {
				uiTask.Status = uiStatusSuccess
				uiTask.Progress = 100
				uiTask.DownloadedBytes = size
				if uiTask.TotalBytes <= 0 {
					uiTask.TotalBytes = size
				}
				uiTask.Phase = "completed"
			}
			a.tasks[uiTask.ID] = uiTask
			a.taskOrder = append(a.taskOrder, uiTask.ID)
			pathIndex[uiTask.Path] = uiTask
			views = append(views, taskToView(uiTask))
			changed = true
		}
		if changed {
			a.recordAutoMergeTasksLocked(drama.ID, built)
			if saveErr := a.saveStateLockedWithDiagnostic("ui.task_enqueue_save_failed"); saveErr != nil {
				a.restoreTaskMutationLocked(snapshot)
				views = views[:viewStart]
				message := "章节已解析，但任务状态保存失败；请检查数据目录并重试：" + a.redactError(saveErr)
				if respErr != "" {
					respErr += "\n"
				}
				respErr += message
				if placeholder := a.tasks[uiTaskID(a.placeholderTask(drama, "parsing").OutPath)]; placeholder != nil {
					placeholder.Status, placeholder.Phase = uiStatusFailed, "failed"
					placeholder.Error = message
					placeholder.UpdatedAt = time.Now()
					failedViews = append(failedViews, taskToView(placeholder))
				}
			} else {
				a.cond.Broadcast()
			}
		}
		a.mu.Unlock()
	}
	return views, failedViews, respErr
}

func (a *UIApp) addParsingPlaceholderTaskLocked(drama Drama, quality int) (uiTaskView, bool) {
	task := a.placeholderTask(drama, "parsing")
	task.DownloadQuality = quality
	uiTask := newUITask(task, time.Now())
	uiTask.Status = uiStatusParsing
	uiTask.Progress = 0
	uiTask.Phase = "parsing"
	uiTask.Title = "正在解析章节"
	if existing := a.tasks[uiTask.ID]; existing != nil {
		existing.Source = task
		existing.Status = uiStatusParsing
		existing.CancelRequested = false
		existing.PauseRequested = false
		existing.RemoveRequested = false
		existing.Progress = 0
		existing.Error = ""
		existing.Phase = "parsing"
		existing.Title = "正在解析章节"
		existing.UpdatedAt = uiTask.UpdatedAt
		return taskToView(existing), false
	}
	a.tasks[uiTask.ID] = uiTask
	a.taskOrder = append(a.taskOrder, uiTask.ID)
	return taskToView(uiTask), true
}

func (a *UIApp) removeParsingPlaceholderTaskLocked(drama Drama) {
	task := a.placeholderTask(drama, "parsing")
	id := uiTaskID(task.OutPath)
	if id == "" {
		return
	}
	if existing := a.tasks[id]; existing == nil || existing.Phase != "parsing" {
		return
	}
	delete(a.tasks, id)
	for i, taskID := range a.taskOrder {
		if taskID == id {
			a.taskOrder = append(a.taskOrder[:i], a.taskOrder[i+1:]...)
			break
		}
	}
}

func (a *UIApp) placeholderTask(drama Drama, suffix string) Task {
	title := drama.DisplayTitle()
	if title == "" {
		title = "短剧"
	}
	dramaID := strings.TrimSpace(drama.ID)
	if dramaID == "" {
		dramaID = title
	}
	dramaDir, err := safeJoin(a.cfg.OutputDir, safeFilename(title))
	if err != nil {
		dramaDir = filepath.Join(a.cfg.OutputDir, safeFilename(title))
	}
	outPath := filepath.Join(dramaDir, ".placeholder-"+hashShort(dramaID)+".txt")
	chapterSource := firstNonEmpty(drama.Source, sourceFromDramaID(dramaID), "unknown")
	return Task{DramaID: dramaID, DramaTitle: title, Chapter: Chapter{ID: providerChapterID(chapterSource, firstNonEmpty(drama.SourceID, dramaID), suffix), Source: chapterSource, Title: "章节处理中", CurrentEpisode: rawEpisode(1)}, Index: 1, Total: 1, OutPath: outPath}
}

func sourceFromDramaID(id string) string {
	if source, _, ok := splitProviderDramaID(id); ok {
		return source
	}
	return ""
}

func (a *UIApp) addFailedPlaceholderTaskLocked(drama Drama, cause error, quality int) (uiTaskView, bool) {
	task := a.placeholderTask(drama, "fetch-failed")
	task.DownloadQuality = quality
	uiTask := newUITask(task, time.Now())
	uiTask.Title = "章节获取失败"
	uiTask.Status = uiStatusFailed
	uiTask.Progress = 0
	uiTask.Error = a.redactError(cause)
	uiTask.Phase = "failed"
	if existing := a.tasks[uiTask.ID]; existing != nil {
		existing.Source = task
		existing.Status = uiStatusFailed
		existing.Title = "章节获取失败"
		existing.Progress = 0
		existing.Error = uiTask.Error
		existing.Phase = "failed"
		existing.UpdatedAt = uiTask.UpdatedAt
		return taskToView(existing), false
	}
	a.tasks[uiTask.ID] = uiTask
	a.taskOrder = append(a.taskOrder, uiTask.ID)
	return taskToView(uiTask), true
}

func (a *UIApp) removeFailedPlaceholderTaskLocked(drama Drama) {
	task := a.placeholderTask(drama, "fetch-failed")
	id := uiTaskID(task.OutPath)
	if id == "" {
		return
	}
	if existing := a.tasks[id]; existing == nil || !isFetchFailedPlaceholderTask(existing) {
		return
	}
	delete(a.tasks, id)
	for i, taskID := range a.taskOrder {
		if taskID == id {
			a.taskOrder = append(a.taskOrder[:i], a.taskOrder[i+1:]...)
			break
		}
	}
}

func isFetchFailedPlaceholderTask(task *UITask) bool {
	return task != nil && task.Phase == "failed" && task.Title == "章节获取失败" && strings.Contains(task.Source.Chapter.ID, ":fetch-failed")
}

func dramaFromPlaceholderTask(task *UITask) Drama {
	if task == nil {
		return Drama{}
	}
	drama := Drama{ID: task.DramaID, Title: task.DramaTitle, Name: task.DramaTitle}
	drama.Source = firstNonEmpty(task.Source.Chapter.Source, sourceFromDramaID(task.DramaID))
	if _, sourceID, ok := splitProviderDramaID(task.DramaID); ok {
		drama.SourceID = sourceID
	} else if task.Source.Chapter.ID != "" {
		parts := strings.Split(task.Source.Chapter.ID, ":")
		if len(parts) >= 2 {
			drama.SourceID = parts[1]
		}
	}
	return drama
}

func (a *UIApp) handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	a.mu.Lock()
	views := a.taskViewsLocked()
	merges := cloneMergeStates(a.merges)
	automatic := cloneAutoMergeStates(a.autoMerges)
	for _, state := range automatic {
		state.RequiredTaskIDs = nil
	}
	for id := range merges {
		merges[id].PlaybackTaskID = ""
		if _, _, err := a.mergedPlaybackTaskLocked(mergedPlaybackPrefix + id); err == nil {
			merges[id].PlaybackTaskID = mergedPlaybackPrefix + id
		}
	}
	a.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"data": views, "merges": merges, "autoMerges": automatic, "ffmpeg": a.downloader.ffmpegInstallation().snapshot()})
}

func (a *UIApp) handleRetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	selection, ok := readTaskSelection(w, r)
	if !ok {
		return
	}
	a.mu.Lock()
	snapshot := a.snapshotTaskMutationLocked()
	if a.parsingInFlight == nil {
		a.parsingInFlight = map[string]bool{}
	}
	now := time.Now()
	changed := false
	var saveErr error
	var reparses []Drama
	for _, id := range selection.idsLocked(a) {
		task := a.tasks[id]
		if task == nil || task.RemoveRequested || task.Status != uiStatusFailed && task.Status != uiStatusCanceled && !a.canRetryUnavailableCompletedTask(task) {
			continue
		}
		if isChapterPlaceholderTask(task) {
			drama := dramaFromPlaceholderTask(task)
			dramaKey := placeholderDramaKey(drama)
			if a.parsingInFlight[dramaKey] {
				continue
			}
			a.parsingInFlight[dramaKey] = true
			a.removeFailedPlaceholderTaskLocked(drama)
			if _, placeholderChanged := a.addParsingPlaceholderTaskLocked(drama, task.Source.DownloadQuality); placeholderChanged {
				changed = true
			}
			reparses = append(reparses, drama)
			changed = true
			continue
		}
		task.Status = uiStatusQueued
		task.Progress = 0
		task.Error = ""
		task.CancelRequested = false
		task.PauseRequested = false
		task.DownloadedBytes = 0
		task.ElapsedSeconds = 0
		task.MediaElapsedSeconds = 0
		task.SpeedBytesPerSecond = 0
		task.RemainingSeconds = 0
		task.Phase = "queued"
		task.NextSpaceCheck = time.Time{}
		task.UpdatedAt = now
		changed = true
	}
	if changed {
		saveErr = a.saveStateLockedWithDiagnostic("ui.task_retry_save_failed")
		if saveErr != nil {
			a.restoreTaskMutationLocked(snapshot)
		} else {
			a.cond.Broadcast()
		}
	}
	views := a.taskViewsLocked()
	a.mu.Unlock()
	if saveErr != nil {
		a.writeStatePersistenceError(w, saveErr)
		return
	}
	for _, drama := range reparses {
		a.startDramaParse(drama, true)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": views, "pending": len(reparses) > 0})
}

func (a *UIApp) handleCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	selection, ok := readTaskSelection(w, r)
	if !ok {
		return
	}
	a.mu.Lock()
	snapshot := a.snapshotTaskMutationLocked()
	var cancels []context.CancelFunc
	now := time.Now()
	changed := false
	var saveErr error
	for _, id := range selection.idsLocked(a) {
		task := a.tasks[id]
		if task == nil {
			continue
		}
		switch task.Status {
		case uiStatusParsing:
			a.disableAutoMergeLocked(task.DramaID)
			task.PauseRequested = false
			task.CancelRequested = true
			if cancel := a.parsingCancels[task.DramaID]; cancel != nil {
				cancels = append(cancels, cancel)
			}
			changed = true
		case uiStatusQueued, uiStatusPaused:
			a.disableAutoMergeLocked(task.DramaID)
			task.PauseRequested = false
			task.Status = uiStatusCanceled
			task.Progress = 0
			task.Error = "已取消"
			task.CancelRequested = false
			task.RemainingSeconds = 0
			task.SpeedBytesPerSecond = 0
			task.Phase = "canceled"
			task.UpdatedAt = now
			changed = true
		case uiStatusRunning:
			a.disableAutoMergeLocked(task.DramaID)
			task.PauseRequested = false
			task.CancelRequested = true
			task.UpdatedAt = now
			if cancel := a.runningCancels[id]; cancel != nil {
				cancels = append(cancels, cancel)
			}
			changed = true
		}
	}
	if changed {
		saveErr = a.saveStateLockedWithDiagnostic("ui.task_cancel_save_failed")
		if saveErr != nil {
			a.restoreTaskMutationLocked(snapshot)
		}
	}
	views := a.taskViewsLocked()
	a.mu.Unlock()
	if saveErr != nil {
		a.writeStatePersistenceError(w, saveErr)
		return
	}
	// long: 取消意图先写入状态文件，再中断任务；写入失败时恢复原状态并保持下载继续运行。
	for _, cancel := range cancels {
		cancel()
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": views})
}

func (a *UIApp) handleClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	ids, ok := readIDsRequest(w, r)
	if !ok {
		return
	}
	a.mu.Lock()
	snapshot := a.snapshotTaskMutationLocked()
	var cancels []context.CancelFunc
	removed, pending := 0, 0
	for _, id := range ids {
		task := a.tasks[id]
		if task == nil {
			continue
		}
		if task.Status == uiStatusRunning || task.Status == uiStatusParsing {
			task.RemoveRequested = true
			task.CancelRequested = true
			if cancel := a.runningCancels[id]; cancel != nil {
				cancels = append(cancels, cancel)
			}
			if task.Status == uiStatusParsing {
				if cancel := a.parsingCancels[task.DramaID]; cancel != nil {
					cancels = append(cancels, cancel)
				}
			}
			pending++
		} else {
			a.removeTaskLocked(id)
			removed++
		}
		a.disableAutoMergeLocked(task.DramaID)
	}
	saveErr := a.saveStateLockedWithDiagnostic("ui.task_clear_save_failed")
	if saveErr != nil {
		a.restoreTaskMutationLocked(snapshot)
	}
	views := a.taskViewsLocked()
	a.mu.Unlock()
	if saveErr != nil {
		a.writeStatePersistenceError(w, saveErr)
		return
	}
	// long: 清理记录的意图写入成功后才中断后台任务，避免请求报错却在重启前悄悄移除任务。
	for _, cancel := range cancels {
		cancel()
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": views, "removed": removed, "pending": pending})
}

func (a *UIApp) handleMerge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	ids, deleteEpisodes, async, ok := readMergeRequest(w, r)
	if !ok {
		return
	}
	a.mu.Lock()
	jobs, err := a.enqueueMergesLocked(ids, deleteEpisodes)
	a.mu.Unlock()
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": a.redactError(err)})
		return
	}
	if async {
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": len(jobs), "async": true})
		return
	}
	results := make([]uiMergeResult, 0, len(jobs))
	persistFailed := false
	var persistError string
	for _, job := range jobs {
		select {
		case <-r.Context().Done():
			return
		case <-job.done:
			results = append(results, job.result)
			if job.result.PersistFailed {
				persistFailed = true
				if persistError == "" {
					persistError = job.result.Error
				}
			}
		}
	}
	if persistFailed {
		// long: 同步请求若无法保存合并结果，必须返回服务错误；结果仍附带全集路径，便于找回已生成的文件。
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": persistError, "code": "state_persist_failed", "data": results})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": results})
}

func (a *UIApp) mergeDrama(ctx context.Context, dramaID string, tasks []*UITask, deleteEpisodes bool) uiMergeResult {
	automatic, _ := ctx.Value(autoMergeContextKey{}).(*autoMergeSnapshot)
	res := uiMergeResult{DramaID: dramaID}
	if len(tasks) == 0 {
		res.Error = "没有已完成的分集可合并"
		a.setMergeState(res, "failed", 0, false, deleteEpisodes)
		return res
	}
	sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].Source.Index < tasks[j].Source.Index })
	valid := make([]*UITask, 0, len(tasks))
	seenEp := map[int]bool{}
	startEp, endEp := 0, 0
	for _, task := range tasks {
		if task == nil || task.Path == "" {
			continue
		}
		// long: 任务路径来自持久化状态，合并前确认文件仍属于当前剧集的受管理目录，避免向其他目录写入全集。
		if task.Source.OutPath != "" && filepath.Clean(task.Source.OutPath) != filepath.Clean(task.Path) || a.validateDeletePath(task.Path, task.DramaID) != nil {
			res.Error = "分集文件路径不在受管理的下载目录中，未执行合并"
			a.setMergeState(res, "failed", 0, false, deleteEpisodes)
			return res
		}
		ep := task.Source.Index
		if ep <= 0 {
			ep = episodeNumber(task.Episode)
		}
		if ep <= 0 || seenEp[ep] {
			continue
		}
		if st, err := os.Stat(task.Path); err == nil && !st.IsDir() && st.Size() > 0 {
			seenEp[ep] = true
			valid = append(valid, task)
			if startEp == 0 || ep < startEp {
				startEp = ep
			}
			if ep > endEp {
				endEp = ep
			}
		}
	}
	if len(valid) == 0 {
		res.Error = "已完成任务的文件不存在"
		a.setMergeState(res, "failed", 0, false, deleteEpisodes)
		return res
	}
	for ep := startEp; ep <= endEp; ep++ {
		if !seenEp[ep] {
			res.DramaTitle = valid[0].DramaTitle
			res.StartEpisode = startEp
			res.EndEpisode = endEp
			res.Merged = len(valid)
			res.Error = fmt.Sprintf("缺少第%03d集，未执行合并", ep)
			a.setMergeState(res, "failed", 0, false, deleteEpisodes)
			return res
		}
	}
	res.DramaTitle = valid[0].DramaTitle
	res.Merged = len(valid)
	res.StartEpisode = startEp
	res.EndEpisode = endEp
	dir := filepath.Dir(valid[0].Path)
	outPath := filepath.Join(dir, mergeOutputName(res.DramaTitle, startEp, endEp))
	if a.validateDeletePath(outPath, dramaID) != nil {
		res.Error = "全集输出路径不在受管理的下载目录中，未执行合并"
		a.setMergeState(res, "failed", 0, false, deleteEpisodes)
		return res
	}
	res.OutputPath = outPath
	if existing, statErr := os.Lstat(outPath); statErr == nil {
		if automatic != nil {
			// long: 自动合并不能把可播放的同名文件当成本轮全集；只有输入集合与输出哈希均匹配的合并收据允许恢复复用。
			if err := verifyAutoMergedReceipt(ctx, dramaID, outPath, automatic); err != nil {
				res.Error = a.redactError(err)
				a.recordMergeError(&res, err, false)
				return res
			}
		}
		// long: 同名文件可能是用户自行放入的媒体；不能只按大小认定为全集，更不能在校验失败后覆盖它。
		if !existing.Mode().IsRegular() || existing.Size() < a.cfg.SkipBytes {
			res.Error = "已有同名全集文件且未通过大小检查，未覆盖；请先移走或删除该文件"
			a.setMergeState(res, "failed", 0, false, deleteEpisodes)
			return res
		}
		verifyCtx, verifyCancel := context.WithTimeout(ctx, 2*time.Hour)
		ffmpeg, probeErr := a.downloader.ensureFFmpeg(verifyCtx)
		if probeErr == nil {
			_, probeErr = probeMergeMedia(verifyCtx, ffmpeg, outPath)
		}
		if probeErr == nil && deleteEpisodes {
			// long: 删除原分集前完整解码旧全集，防止外部替换或截断的文件只通过容器检查。
			probeErr = decodeMergedMedia(verifyCtx, ffmpeg, outPath, nil)
		}
		verifyCancel()
		if probeErr != nil {
			// long: 包装可播放性诊断时保留取消原因，既提示同名文件需处理，也让正常停服继续恢复自动意图。
			err := fmt.Errorf("已有同名全集文件但无法确认可播放，未覆盖；请先检查或移走该文件：%w", probeErr)
			a.recordMergeError(&res, err, deleteEpisodes)
			return res
		}
		a.finishMergedDrama(&res, valid, true, deleteEpisodes)
		return res
	} else if !errors.Is(statErr, os.ErrNotExist) {
		res.Error = "检查全集输出路径失败：" + a.redactError(statErr)
		a.setMergeState(res, "failed", 0, false, deleteEpisodes)
		return res
	}

	paths := make([]string, 0, len(valid))
	var reserve uint64
	for _, task := range valid {
		paths = append(paths, task.Path)
		info, err := os.Stat(task.Path)
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
			res.Error = "合并前分集文件已变化，原文件已保留"
			a.setMergeState(res, "failed", 0, false, deleteEpisodes)
			return res
		}
		reserve += uint64(info.Size())
	}
	// long: 空间不足返回后台队列等待，合并的两小时超时只计算实际处理时间。
	if err := a.downloader.requireSpace(dir, reserve); err != nil {
		a.recordMergeError(&res, err, deleteEpisodes)
		return res
	}
	// long: 每次合并使用独立临时文件，不能清掉用户目录里碰巧同名的 .part.mp4。
	partFile, err := os.CreateTemp(dir, ".duanju-merge-*.part.mp4")
	if err != nil {
		a.recordMergeError(&res, fmt.Errorf("创建合并临时文件失败：%w", err), deleteEpisodes)
		return res
	}
	partPath := partFile.Name()
	defer os.Remove(partPath)
	// long: 全集可能由独立的媒体服务读取；临时文件的默认 0600 权限不能被硬链接继承到最终成品。
	modeErr := partFile.Chmod(0o664)
	closeErr := partFile.Close()
	if modeErr != nil || closeErr != nil || a.validateDeletePath(partPath, dramaID) != nil {
		res.Error = "合并临时文件不在受管理的下载目录中，未执行合并"
		a.setMergeState(res, "failed", 0, false, deleteEpisodes)
		return res
	}
	cmdCtx, cancel := context.WithTimeout(ctx, 2*time.Hour)
	spaceResult := diskspace.Watch(cmdCtx, dir, cancel)
	defer func() { cancel(); <-spaceResult }()
	res.Detail = "正在准备 FFmpeg"
	if err := a.setMergeState(res, "running", 0, false, deleteEpisodes); err != nil {
		a.recordMergeError(&res, err, deleteEpisodes)
		return res
	}
	ffmpeg, err := a.downloader.ensureFFmpeg(cmdCtx)
	if err != nil {
		cancel()
		if spaceErr := <-spaceResult; spaceErr != nil {
			err = spaceErr
		}
		a.recordMergeError(&res, err, deleteEpisodes)
		return res
	}
	method, err := mergeMediaFiles(cmdCtx, ffmpeg, paths, partPath, func(progress int, detail string) {
		res.Detail = detail
		a.setMergeState(res, "running", progress, false, deleteEpisodes)
	})
	commandErr := cmdCtx.Err()
	cancel()
	if spaceErr := <-spaceResult; spaceErr != nil {
		err = spaceErr
	}
	if err != nil {
		a.recordMergeError(&res, err, deleteEpisodes)
		return res
	}
	res.Detail = method
	if commandErr != nil || ctx.Err() != nil {
		a.recordMergeError(&res, context.Canceled, deleteEpisodes)
		return res
	}
	if ok, _ := existingGood(partPath, a.cfg.SkipBytes); !ok {
		res.Error = "合并输出文件无效"
		a.setMergeState(res, "failed", 0, false, deleteEpisodes)
		return res
	}
	// long: 发布全集不能覆盖合并期间出现的同名文件；优先原子链接，不支持硬链接的存储改为独占复制。
	if automatic != nil {
		if err := prepareAutoMergedReceipt(ctx, dramaID, outPath, partPath, automatic); err != nil {
			res.Error = a.redactError(err)
			a.recordMergeError(&res, err, false)
			return res
		}
	}
	if err := ctx.Err(); err != nil {
		a.recordMergeError(&res, err, deleteEpisodes)
		return res
	}
	if err := publishMergedFile(partPath, outPath); err != nil {
		if diskspace.IsWaiting(err) {
			a.recordMergeError(&res, err, deleteEpisodes)
			return res
		}
		if errors.Is(err, os.ErrExist) {
			res.Error = "合并期间出现同名全集文件，未覆盖；请检查该文件后重试"
		} else {
			res.Error = "安全发布全集失败：" + a.redactError(err)
		}
		a.setMergeState(res, "failed", 0, false, deleteEpisodes)
		return res
	}
	a.finishMergedDrama(&res, valid, false, deleteEpisodes)
	return res
}

func publishMergedFile(source, target string) error {
	if err := os.Link(source, target); err == nil {
		return nil
	} else if errors.Is(err, os.ErrExist) {
		return err
	}
	// long: 部分 NAS 文件系统不支持硬链接；O_EXCL 保证回退复制也不会替换用户已有的全集文件。
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if err := diskspace.Require(target, uint64(info.Size())); err != nil {
		return err
	}
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	created, statErr := output.Stat()
	finished := false
	defer func() {
		if finished || statErr != nil {
			return
		}
		// long: 复制失败时只移除本次创建的不完整文件，避免并发进程已经替换路径后误删它的文件。
		if current, err := os.Stat(target); err == nil && os.SameFile(current, created) {
			_ = os.Remove(target)
		}
	}()
	_, copyErr := io.Copy(output, &diskspace.Reader{Reader: input, Path: target})
	if copyErr == nil {
		copyErr = output.Sync()
	}
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	finished = true
	return nil
}

func (a *UIApp) finishMergedDrama(res *uiMergeResult, valid []*UITask, skipped, deleteEpisodes bool) {
	res.Skipped = skipped
	if !deleteEpisodes {
		if err := a.setMergeState(*res, "success", 100, skipped, false); err != nil {
			res.Error = "全集已生成，但合并记录保存失败；原分集已保留：" + a.redactError(err)
			res.PersistFailed = true
			return
		}
		res.OK = true
		a.recordMergeCompleted(*res, skipped, deleteEpisodes)
		return
	}
	// long: 成功全集和全部待删路径先落盘，再删除原分集；即使中途断电或保存失败，重启后仍能找到全集并补做清理。
	for _, task := range valid {
		res.CleanupPending = append(res.CleanupPending, task.Path)
	}
	if err := a.setMergeState(*res, "success", 100, skipped, true); err != nil {
		res.Error = "全集已生成，但合并记录保存失败；原分集已保留：" + a.redactError(err)
		res.PersistFailed = true
		return
	}
	res.CleanupPending = nil
	a.cleanupMergedEpisodeFiles(res, valid)
	if err := a.setMergeState(*res, "success", 100, skipped, true); err != nil {
		res.Error = "全集已生成，但清理结果保存失败；重启后可重试清理：" + a.redactError(err)
		res.PersistFailed = true
		return
	}
	res.OK = true
	a.recordMergeCompleted(*res, skipped, deleteEpisodes)
}

func (a *UIApp) recordMergeCompleted(res uiMergeResult, skipped, deleteEpisodes bool) {
	a.downloader.recordDiagnostic(diagnosticEvent{Level: "info", Event: "merge.completed", DramaID: res.DramaID, DramaTitle: res.DramaTitle,
		Episode: res.StartEpisode, Message: fmt.Sprintf("episodes=%d-%d merged=%d skipped=%t deleteEpisodes=%t cleanupPending=%d", res.StartEpisode, res.EndEpisode, res.Merged, skipped, deleteEpisodes, len(res.CleanupPending))})
}

func (a *UIApp) cleanupMergedEpisodeFiles(res *uiMergeResult, valid []*UITask) {
	// long: 全集校验通过后才清理分集；任何失败都保留待重试路径，不能把合并成功误报为已回收空间。
	a.mu.Lock()
	defer a.mu.Unlock()
	ids := make([]string, 0, len(valid))
	for _, task := range valid {
		ids = append(ids, task.ID)
	}
	referenceErr := a.validateDeleteReferencesLocked(ids, nil)
	for _, task := range valid {
		current := a.tasks[task.ID]
		// long: 合并使用提交时的分集快照；若期间任务被重试或路径变化，旧快照不能删除新下载正在使用的文件。
		if referenceErr != nil || current == nil || current.Status != uiStatusSuccess || current.RemoveRequested ||
			filepath.Clean(firstNonEmpty(current.Path, current.Source.OutPath)) != filepath.Clean(task.Path) ||
			current.Source.OutPath != "" && filepath.Clean(current.Source.OutPath) != filepath.Clean(task.Path) ||
			filepath.Clean(task.Path) == filepath.Clean(res.OutputPath) {
			res.CleanupPending = append(res.CleanupPending, task.Path)
			continue
		}
		if _, err := a.deleteManagedPath(task.Path, task.DramaID); err != nil {
			res.CleanupPending = append(res.CleanupPending, task.Path)
		}
	}
	if len(res.CleanupPending) > 0 {
		res.Error = fmt.Sprintf("全集已生成，但 %d 个分集文件清理失败；可在下载页重试清理", len(res.CleanupPending))
	}
}

func cloneMergeStates(in map[string]*UIMergeState) map[string]*UIMergeState {
	out := make(map[string]*UIMergeState, len(in))
	for id, state := range in {
		if state == nil {
			continue
		}
		copyState := *state
		out[id] = &copyState
	}
	return out
}

func mergeOutputName(title string, start, end int) string {
	if start <= 0 {
		start = 1
	}
	if end < start {
		end = start
	}
	return fmt.Sprintf("%s%d-%d.mp4", safeFilename(title), start, end)
}

func (a *UIApp) setMergeState(res uiMergeResult, status string, progress int, skipped, deleteEpisodes bool) error {
	state := &UIMergeState{
		ResumeAutomatic: res.ResumeAutomatic,
		WaitingForSpace: res.WaitingForSpace,
		DramaID:         res.DramaID, DramaTitle: res.DramaTitle, Status: status, Progress: progress,
		Merged: res.Merged, StartEpisode: res.StartEpisode, EndEpisode: res.EndEpisode,
		Total: res.Merged, OutputPath: res.OutputPath, Detail: res.Detail, Error: res.Error, Skipped: skipped,
		DeleteEpisodes: deleteEpisodes && (res.WaitingForSpace || status == "success" && len(res.CleanupPending) == 0),
		CleanupPending: append([]string(nil), res.CleanupPending...), UpdatedAt: time.Now(),
	}
	a.mu.Lock()
	if job := a.mergeJobs[res.DramaID]; job != nil {
		state.Automatic = job.autoGeneration != 0
		if job.userCanceled && status != "success" {
			// long: 关闭与错误回调可能交错，最终写入必须在同一锁内复查取消；已发布成功的全集仍保留成功记录。
			status, state.Status, state.Progress = "failed", "failed", 0
			state.WaitingForSpace, state.ResumeAutomatic, state.DeleteEpisodes = false, false, false
			state.Detail, state.Error = "", "合并已取消，原分集已保留"
		}
	}
	if a.merges == nil {
		a.merges = map[string]*UIMergeState{}
	}
	a.merges[res.DramaID] = state
	err := a.saveStateLockedWithDiagnostic("ui.merge_state_save_failed")
	if err != nil && status == "success" {
		state.Error = "全集已生成，但状态保存失败；请检查数据目录后重试清理"
	}
	a.mu.Unlock()
	if err == nil && (status == "success" || status == "failed") {
		a.notifyEmbyMerge(res.DramaID)
	}
	return err
}

func readMergeRequest(w http.ResponseWriter, r *http.Request) ([]string, bool, bool, bool) {
	var req struct {
		Async          bool     `json:"async"`
		DramaIDs       []string `json:"dramaIds"`
		DeleteEpisodes bool     `json:"deleteEpisodes"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
		return nil, false, false, false
	}
	ids, ok := cleanIDList(w, req.DramaIDs)
	return ids, req.DeleteEpisodes, req.Async, ok
}

func (a *UIApp) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if !a.updateRuntimeSettings(w, r) {
			return
		}
	} else if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	a.mu.Lock()
	addr := a.address
	cfg := a.cfg
	nextOutputDir := firstNonEmpty(a.nextOutputDir, cfg.outputDirSetting, cfg.OutputDir)
	a.mu.Unlock()
	proxyMode, proxyURL, proxyHasAuth := publicProxyConfig(cfg.ProxyURL)
	writeJSON(w, http.StatusOK, map[string]any{"outputDir": cfg.OutputDir, "outputDirSetting": nextOutputDir, "restartRequired": !sameDirectory(cfg.OutputDir, nextOutputDir), "dataDir": cfg.dataDirectory(), "concurrency": cfg.Concurrency, "ffmpeg": cfg.FFmpeg, "address": addr, "libraryCachePath": libraryCachePath(cfg.dataDirectory()), "requestConcurrency": cfg.RequestConcurrency, "requestIntervalMs": cfg.RequestIntervalMS, "network": a.downloader.proxyRouter.summary(), "proxyMode": proxyMode, "proxyURL": proxyURL, "proxyHasAuth": proxyHasAuth})
}

func readIDsRequest(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	var req struct {
		IDs []string `json:"ids"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
		return nil, false
	}
	return cleanIDList(w, req.IDs)
}

func readDramaIDsRequest(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	var req struct {
		DramaIDs []string `json:"dramaIds"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
		return nil, false
	}
	return cleanIDList(w, req.DramaIDs)
}

func cleanIDList(w http.ResponseWriter, in []string) ([]string, bool) {
	seen := map[string]bool{}
	ids := make([]string, 0, len(in))
	for _, id := range in {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ids is required"})
		return nil, false
	}
	return ids, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func uiTaskTimeout(task Task) time.Duration {
	total := task.Total
	if total <= 0 {
		total = 1
	}

	timeout := time.Duration(20+total*3) * time.Minute
	if timeout < 30*time.Minute {
		return 30 * time.Minute
	}
	if timeout > 2*time.Hour {
		return 2 * time.Hour
	}
	return timeout
}

func newUITask(task Task, now time.Time) *UITask {
	episode := task.Chapter.EpisodeString(task.Index)
	title := strings.TrimSpace(task.Chapter.Title)
	if title == "" || strings.TrimSpace(title) == strings.TrimSpace(episode) {
		title = "第" + padEpisode(episode) + "集"
	}
	return &UITask{
		ID: uiTaskID(task.OutPath), DramaID: task.DramaID, DramaTitle: task.DramaTitle,
		Episode: episode, Title: title, Status: uiStatusQueued, Progress: 0,
		TotalBytes: task.Chapter.MediaSize, Phase: "queued", Path: task.OutPath,
		CreatedAt: now, UpdatedAt: now, Source: task,
	}
}

func uiTaskID(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:])[:16]
}

func cloneUITask(task *UITask) *UITask {
	if task == nil {
		return nil
	}
	cp := *task
	return &cp
}

func (a *UIApp) pathIndexLocked() map[string]*UITask {
	out := make(map[string]*UITask, len(a.tasks))
	for _, task := range a.tasks {
		if task == nil {
			continue
		}
		if task.Path != "" {
			out[task.Path] = task
		}
		if task.Source.OutPath != "" {
			out[task.Source.OutPath] = task
		}
	}
	return out
}

func (a *UIApp) taskViewsLocked() []uiTaskView {
	views := make([]uiTaskView, 0, len(a.taskOrder))
	releases := make(map[string]string, len(a.dramas))
	for _, drama := range a.dramas {
		releases[drama.ID] = dramaReleaseStatus(drama)
	}
	for _, id := range a.taskOrder {
		if task := a.tasks[id]; task != nil {
			view := taskToView(task)
			if a.completedFileUnavailable(task) {
				// long: 文件可访问性只写入接口视图；共享存储暂不可用时不能把成功任务持久化成失败。
				view.LocalFileUnavailable = true
				view.LocalFileRetryable = a.canRetryUnavailableCompletedTaskAfterCheck(task)
			}
			if status := releases[task.DramaID]; status != "" {
				view.ReleaseStatus = status
			}
			views = append(views, view)
		}
	}
	return views
}

func taskToView(task *UITask) uiTaskView {
	return uiTaskView{
		ID: task.ID, DramaID: task.DramaID, DramaTitle: task.DramaTitle, Episode: task.Episode, Title: task.Title,
		Index: task.Source.Index, Total: task.Source.Total, Status: task.Status, Progress: task.Progress, Error: task.Error,
		Path: task.Path, Attempt: task.Attempt, DownloadedBytes: task.DownloadedBytes, TotalBytes: task.TotalBytes,
		SpeedBytesPerSecond: task.SpeedBytesPerSecond, ElapsedSeconds: task.ElapsedSeconds, RemainingSeconds: task.RemainingSeconds,
		MediaElapsedSeconds: task.MediaElapsedSeconds, MediaTotalSeconds: task.MediaTotalSeconds, Phase: task.Phase,
		CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt, CancelRequested: task.CancelRequested,
		RemoveRequested: task.RemoveRequested, DeleteFilesRequested: task.DeleteFilesRequested, PauseRequested: task.PauseRequested, ReleaseStatus: task.Source.ReleaseStatus,
		Playable: isPlayableDownloadTask(task), DownloadQuality: task.Source.DownloadQuality,
	}
}

func (a *UIApp) redactError(err error) string {
	if err == nil {
		return ""
	}
	return a.redactString(err.Error())
}

func (a *UIApp) redactString(value string) string { return redactErrorString(value) }
