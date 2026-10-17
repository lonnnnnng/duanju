package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type autoMergeContextKey struct{}

type autoMergeInput struct {
	ID     string `json:"id"`
	Index  int    `json:"index"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	info   os.FileInfo
}

type autoMergeSnapshot struct {
	Inputs []autoMergeInput
	Digest string
}

type autoMergeReceipt struct {
	Version      int    `json:"version"`
	DramaID      string `json:"dramaId"`
	InputSHA256  string `json:"inputSha256"`
	OutputSHA256 string `json:"outputSha256"`
}

func collectAutoMergeInputs(ctx context.Context, tasks []*UITask) (*autoMergeSnapshot, error) {
	snapshot := &autoMergeSnapshot{}
	for _, task := range tasks {
		file, info, err := hashEmbyLocalFile(ctx, task.Path, embyLocalMergedName)
		if err != nil {
			return nil, err
		}
		if file == nil {
			return nil, errors.New("自动合并分集已移动，原内容已保留")
		}
		snapshot.Inputs = append(snapshot.Inputs, autoMergeInput{ID: task.ID, Index: task.Source.Index, Path: filepath.Clean(task.Path), SHA256: file.SHA256, info: info})
	}
	body, err := json.Marshal(snapshot.Inputs)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	snapshot.Digest = hex.EncodeToString(sum[:])
	return snapshot, nil
}

func (snapshot *autoMergeSnapshot) unchanged(ctx context.Context) error {
	for _, input := range snapshot.Inputs {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err := os.Lstat(input.Path)
		if err != nil || !sameEmbyFileSnapshot(input.info, current) {
			return errors.New("合并期间分集内容或身份发生变化，原文件已保留")
		}
		if embyMediaIdentity(current) == "" {
			// long: 没有可靠 ctime 的平台在发布前再读分集内容，不能仅依赖可被还原的 mtime。
			actual, _, err := hashEmbyLocalFile(ctx, input.Path, embyLocalMergedName)
			if err != nil {
				return err
			}
			if actual == nil || actual.SHA256 != input.SHA256 {
				return errors.New("合并期间分集内容发生变化，原文件已保留")
			}
		}
	}
	return nil
}

func readAutoMergedReceipt(path, dramaID string) (autoMergeReceipt, error) {
	var receipt autoMergeReceipt
	info, err := os.Lstat(path + ".duanju-merge.json")
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return receipt, errors.New("同名全集缺少本程序的自动合并收据，原文件已保留；请移走后重试")
	}
	if err := readEmbyJSON(path+".duanju-merge.json", &receipt, 4096); err != nil {
		return receipt, err
	}
	input, inputErr := hex.DecodeString(receipt.InputSHA256)
	output, outputErr := hex.DecodeString(receipt.OutputSHA256)
	if receipt.Version != 1 || receipt.DramaID != dramaID || inputErr != nil || outputErr != nil || len(input) != sha256.Size || len(output) != sha256.Size {
		return receipt, errors.New("同名自动合并收据不是本剧生成，原内容已保留")
	}
	return receipt, nil
}

func verifyAutoMergedReceipt(ctx context.Context, dramaID, path string, snapshot *autoMergeSnapshot) error {
	receipt, err := readAutoMergedReceipt(path, dramaID)
	if err != nil {
		return err
	}
	if receipt.InputSHA256 != snapshot.Digest {
		return errors.New("同名全集对应另一组分集，原文件已保留；请移走后重试")
	}
	actual, _, err := hashEmbyLocalFile(ctx, path, embyLocalMergedName)
	if err != nil {
		return err
	}
	if actual == nil || actual.SHA256 != receipt.OutputSHA256 {
		return errors.New("已有全集与自动合并收据不一致，原内容已保留")
	}
	return snapshot.unchanged(ctx)
}

func prepareAutoMergedReceipt(ctx context.Context, dramaID, path, stage string, snapshot *autoMergeSnapshot) error {
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return errors.New("发布期间出现同名全集，原文件已保留")
	}
	if err := snapshot.unchanged(ctx); err != nil {
		return err
	}
	actual, _, err := hashEmbyLocalFile(ctx, stage, embyLocalMergedName)
	if err != nil {
		return err
	}
	if actual == nil {
		return errors.New("自动合并成品已移动，未发布")
	}
	if _, err := os.Lstat(path + ".duanju-merge.json"); err == nil {
		if _, err := readAutoMergedReceipt(path, dramaID); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// long: 先持久化分集集合与成品哈希再发布 MP4；进程在发布后退出仍可证明同名全集对应本轮分集。
	if err := ctx.Err(); err != nil {
		return err
	}
	return writeEmbyJSON(path+".duanju-merge.json", autoMergeReceipt{Version: 1, DramaID: dramaID, InputSHA256: snapshot.Digest, OutputSHA256: actual.SHA256}, 0600)
}

func removeAutoMergedReceipt(path, dramaID string) error {
	name := path + ".duanju-merge.json"
	info, err := os.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := readAutoMergedReceipt(path, dramaID); err != nil {
		return nil
	}
	current, err := os.Lstat(name)
	if err != nil || !sameEmbyFileSnapshot(info, current) {
		return errors.New("自动合并收据在删除时变化，原内容已保留")
	}
	// long: 用户删除已登记的全集时一并回收可确认属于本剧的收据，同名用户元数据保留。
	return os.Remove(name)
}
