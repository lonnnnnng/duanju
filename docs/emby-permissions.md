# Emby 媒体目录权限

## 适用场景

duanju 以 Linux 用户 `lee` 运行，Emby 以用户 `emby` 运行，两者需要访问同一个目录：

```text
/home/lee/duanju/downloads/Emby
```

Linux 同时检查每一级父目录的“进入”权限和目标目录的读写权限。即使 `Emby` 目录本身是 `emby:media`，只要 `/home/lee/duanju` 没有允许 `emby` 穿过，Emby 仍然会提示没有权限。

本项目使用以下边界：

- `media` 组由 `lee` 和 `emby` 共同使用；
- `downloads` 使用 `2770`，不对所有本机用户开放；
- `Emby` 及其子目录使用 setgid，目录通常为 `2770`，文件通常为 `0660`；
- 只在必要的父目录上给 `emby` 增加 `--x` ACL；
- 不使用无条件的 `chmod -R 777`；
- 修改组成员后必须重启已经运行的服务，进程才会加载新补充组。

## 一键处理

在目标 Linux 服务器下载并执行脚本。脚本需要 `sudo`、`setfacl`、`getfacl` 和 `systemd`：

```bash
curl -fsSL https://raw.githubusercontent.com/lonnnnnng/duanju/main/scripts/configure-emby-permissions.sh \
  -o /tmp/configure-emby-permissions.sh
chmod 700 /tmp/configure-emby-permissions.sh
DUANJU_USER=lee EMBY_USER=emby \
  DUANJU_OUTPUT_DIR=/home/lee/duanju/downloads \
  /tmp/configure-emby-permissions.sh
```

脚本会：

1. 创建 `media` 组（组已存在时复用）。
2. 将 duanju 用户和 Emby 用户加入 `media` 组。
3. 创建或修正 `downloads` 和 `downloads/Emby` 的属主、共享组、组写权限和 setgid。
4. 只给 Emby 穿过 `/home/lee`、`/home/lee/duanju` 等必要父目录的权限。
5. 重启正在运行的 Emby 和 duanju systemd 服务。
6. 使用 Emby 运行用户实际创建并删除临时文件，验证读写，而不是只看 `ls` 输出。

如果需要手动重启服务，可使用：

```bash
sudo systemctl restart emby-server
sudo systemctl restart duanju
```

## 手动步骤

不使用脚本时，按以下顺序执行。把变量替换成当前机器的真实用户和目录：

```bash
DUANJU_USER=lee
EMBY_USER=emby
MEDIA_GROUP=media
OUTPUT_DIR=/home/lee/duanju/downloads
EMBY_DIR="$OUTPUT_DIR/Emby"
```

创建共享组并加入用户：

```bash
getent group "$MEDIA_GROUP" >/dev/null || sudo groupadd "$MEDIA_GROUP"
sudo usermod -aG "$MEDIA_GROUP" "$DUANJU_USER"
sudo usermod -aG "$MEDIA_GROUP" "$EMBY_USER"
```

设置下载根目录和 Emby 目录：

```bash
sudo install -d -o "$DUANJU_USER" -g "$MEDIA_GROUP" -m 2770 "$OUTPUT_DIR"
sudo install -d -o "$DUANJU_USER" -g "$MEDIA_GROUP" -m 2770 "$EMBY_DIR"
sudo chown -R "$DUANJU_USER:$MEDIA_GROUP" "$EMBY_DIR"
sudo find "$EMBY_DIR" -type d -exec chmod 2770 {} +
sudo find "$EMBY_DIR" -type f -exec chmod 0660 {} +
```

给 Emby 增加父目录穿越权限。不要把整个 `/home/lee` 或下载目录改成 `777`：

```bash
sudo setfacl -m u:emby:--x /home/lee
sudo setfacl -m u:emby:--x /home/lee/duanju
```

重启进程并验证：

```bash
sudo systemctl restart emby-server duanju
pid=$(pgrep -xo EmbyServer)
grep '^Groups:' "/proc/$pid/status"
sudo -u emby test -r "$EMBY_DIR"
sudo -u emby test -w "$EMBY_DIR"
sudo -u emby sh -c 'set -eu; f="$1/.duanju-permissions-check.$$"; trap '\''rm -f "$f"'\'' EXIT; umask 007; : > "$f"' sh "$EMBY_DIR"
```

最后在 Emby 中填写：

```text
/home/lee/duanju/downloads/Emby
```

## 检查与回滚

查看每一级路径权限和 ACL：

```bash
namei -l /home/lee/duanju/downloads/Emby
getfacl -p /home/lee /home/lee/duanju /home/lee/duanju/downloads /home/lee/duanju/downloads/Emby
id lee
id emby
```

确认 `downloads` 没有被放开：

```bash
stat -c '%A %a %U:%G %n' /home/lee/duanju/downloads /home/lee/duanju/downloads/Emby
```

预期至少应满足：`downloads` 为 `lee:media`、`2770`，Emby 目录为共享组可读写并带 setgid；同步清单等普通文件为共享组可读写且不对其他用户开放。

如果以后不再需要父目录 ACL，可逐条删除本脚本添加的 ACL。删除前确认 Emby 已通过其他路径权限访问：

```bash
sudo setfacl -x u:emby /home/lee
sudo setfacl -x u:emby /home/lee/duanju
```

不要删除 `media` 组或从组中移除用户，除非同时改成另一套已验证的共享权限方案。升级安装器会保留已有 `data/`、`downloads/` 的属主、属组、ACL 和 setgid。
