package app

// long: 构建脚本会注入正式版本信息；源码运行时保留可识别的开发版本，便于诊断和更新判断。
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

const defaultGitHubRepository = "lonnnnnng/duanju"
