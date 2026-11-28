# 把 cmd/qqavatar 生成的 QQ 头像对照表上传到 R2 (共 512 个文件, 每个约 67MB)
# 已传完的文件记在 <Dir>\.uploaded, 中断后重跑会跳过
#
#   pwsh scripts\qqavatar-upload.ps1 -Dir D:\qqavatar            # 传到线上桶
#   pwsh scripts\qqavatar-upload.ps1 -Dir D:\qqavatar -Local     # 传到 wrangler dev 的本地模拟桶
param(
	[Parameter(Mandatory)][string]$Dir,
	[string]$Bucket = 'proxy-go-qqavatar',
	[int]$Parallel = 6,
	[switch]$Local
)
$ErrorActionPreference = 'Stop'

$worker = Join-Path $PSScriptRoot '..\cloudflare-worker'
$log = Join-Path $Dir '.uploaded'
$done = @{}
if (Test-Path $log) { Get-Content $log | ForEach-Object { $done[$_] = $true } }
$target = if ($Local) { '--local' } else { '--remote' }
# 本地模拟桶是 SQLite, 并发写会撞锁
if ($Local) { $Parallel = 1 }

$files = foreach ($algo in 'md5', 'sha256') {
	Get-ChildItem (Join-Path $Dir $algo) -Filter '*.bin' | ForEach-Object {
		[pscustomobject]@{ Key = "$algo/$($_.Name)"; Path = $_.FullName }
	}
}
$todo = @($files | Where-Object { -not $done.ContainsKey($_.Key) })
Write-Host "共 $($files.Count) 个, 待上传 $($todo.Count) 个"

$lock = [System.Threading.Mutex]::new($false)
$todo | ForEach-Object -ThrottleLimit $Parallel -Parallel {
	$f = $_
	Push-Location $using:worker
	try {
		$out = npx wrangler r2 object put "$($using:Bucket)/$($f.Key)" --file $f.Path --content-type application/octet-stream $using:target 2>&1
		if ($LASTEXITCODE -ne 0) { throw "上传失败: $($f.Key)`n$(($out | Select-Object -Last 8) -join "`n")" }
	} finally { Pop-Location }
	$m = $using:lock
	[void]$m.WaitOne()
	try { Add-Content -Path $using:log -Value $f.Key } finally { $m.ReleaseMutex() }
	Write-Host "已传 $($f.Key)"
}
