# 把 Docker Hub 凭据写入 GitHub Actions Secrets
#
# 为什么需要你手动运行：
#   Docker Hub 的 Access Token 只在你登录后的网页上显示一次，任何自动化读取
#   都等同于抓取你的凭据，所以这一步必须由你本人完成。
#
# 这个脚本保证三件事：
#   1. Token 用隐藏输入读取，不在终端回显；
#   2. 不写入任何文件、不进入 shell 历史；
#   3. 结束后只在内存里清空变量，并打印 secret 名称（不会打印值）。
#
# 运行方式：右键"使用 PowerShell 运行"，或在终端执行
#   powershell -ExecutionPolicy Bypass -File .\set-docker-secrets.ps1

$ErrorActionPreference = 'Stop'
$repo = 'MineXine/Artalk'

Write-Host ''
Write-Host '=== 配置 Docker Hub 凭据到 GitHub Secrets ===' -ForegroundColor Cyan
Write-Host "仓库: $repo"
Write-Host ''

# 前置检查：gh 是否可用且已登录
$null = gh auth status 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Host 'gh 未登录，请先执行: gh auth login' -ForegroundColor Red
    Read-Host '按回车退出'
    return
}

# 1. Docker Hub 用户名（登录 hub.docker.com 后，右上角头像里能看到）
$user = Read-Host 'Docker Hub 用户名'
if ([string]::IsNullOrWhiteSpace($user)) {
    Write-Host '用户名不能为空。' -ForegroundColor Red
    Read-Host '按回车退出'
    return
}
$user = $user.Trim()

# 2. Access Token（隐藏输入）
$secure = Read-Host 'Docker Hub Access Token（粘贴后回车，输入不回显）' -AsSecureString
$bstr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
try {
    $token = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($bstr)
}
finally {
    [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr)
}

if ([string]::IsNullOrWhiteSpace($token)) {
    Write-Host 'Token 不能为空。' -ForegroundColor Red
    Read-Host '按回车退出'
    return
}
# 常见误操作：粘贴时带进了换行或空格
$token = $token.Trim()

Write-Host ''
Write-Host '正在写入...' -ForegroundColor Yellow

# 用 --body 传递而不是管道：管道会给值带上换行，导致 docker login 失败。
# 代价是值会在本机的进程命令行里短暂出现，个人机器上可以接受。
gh secret set DOCKERHUB_USERNAME --repo $repo --body $user
if ($LASTEXITCODE -ne 0) {
    Write-Host '用户名写入失败。' -ForegroundColor Red
    Read-Host '按回车退出'
    return
}

gh secret set DOCKERHUB_TOKEN --repo $repo --body $token
if ($LASTEXITCODE -ne 0) {
    Write-Host 'Token 写入失败。' -ForegroundColor Red
    Read-Host '按回车退出'
    return
}

# 清掉内存里的副本
Remove-Variable token, secure -ErrorAction SilentlyContinue

Write-Host ''
Write-Host '写入完成。当前仓库的 secrets：' -ForegroundColor Green
gh secret list --repo $repo

Write-Host ''
Write-Host '接下来可以手动触发镜像构建：' -ForegroundColor Cyan
Write-Host "  gh workflow run build-docker.yml --repo $repo"
Write-Host ''

Read-Host '按回车关闭窗口'
