param(
  [int]$Port = 5188,
  [switch]$NoBrowser
)

$ErrorActionPreference = "Stop"

$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location $projectRoot

function Test-PortAvailable {
  param([int]$CandidatePort)

  $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, $CandidatePort)
  try {
    $listener.Start()
    return $true
  } catch {
    return $false
  } finally {
    $listener.Stop()
  }
}

function Get-AvailablePort {
  param([int]$StartPort)

  for ($candidate = $StartPort; $candidate -lt ($StartPort + 100); $candidate++) {
    if (Test-PortAvailable -CandidatePort $candidate) {
      return $candidate
    }
  }

  throw "从端口 $StartPort 开始的 100 个端口都不可用，请指定 -Port。"
}

if (-not (Get-Command python -ErrorAction SilentlyContinue)) {
  throw "未找到 python。请先安装 Python，或手动用其它静态服务器打开 index.html。"
}

$actualPort = Get-AvailablePort -StartPort $Port
$url = "http://127.0.0.1:$actualPort/"

Write-Host "o-Picturehtml 本地服务启动中..." -ForegroundColor Cyan
Write-Host "项目目录：$projectRoot"
Write-Host "访问地址：$url" -ForegroundColor Green
Write-Host "停止方式：在本窗口按 Ctrl+C"

if (-not $NoBrowser) {
  Start-Process $url | Out-Null
}

python -m http.server $actualPort --bind 127.0.0.1
