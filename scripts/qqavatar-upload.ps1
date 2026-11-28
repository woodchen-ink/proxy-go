# 把 cmd/qqavatar 生成的 QQ 头像对照表上传到 R2 (共 512 个文件, 每个约 67MB)
# 已传完的文件记在 <Dir>\.uploaded, 中断后重跑会跳过
#
#   pwsh scripts\qqavatar-upload.ps1 -Dir D:\qqavatar                    # 传到线上桶, 不限速
#   pwsh scripts\qqavatar-upload.ps1 -Dir D:\qqavatar -LimitMBps 4.5     # 限速 (需管理员)
#   pwsh scripts\qqavatar-upload.ps1 -Dir D:\qqavatar -Local             # 传到 wrangler dev 的本地模拟桶
#
# 限速: wrangler 没有限速选项, 用一条临时 QoS 策略限制 node.exe 的上行 (只在本次开机有效, 结束时删除),
# 另按本轮均速控节奏兜底; 窗口被直接关掉时策略会留到重启, 手动删除:
#   Remove-NetQosPolicy -Name qqavatar-upload -PolicyStore ActiveStore -Confirm:$false
param(
	[Parameter(Mandatory)][string]$Dir,
	[string]$Bucket = 'proxy-go-qqavatar',
	[int]$Parallel = 6,
	[double]$LimitMBps = 0,
	[switch]$Local
)
$ErrorActionPreference = 'Stop'
$qosName = 'qqavatar-upload'

$Dir = (Resolve-Path $Dir).Path
$worker = Join-Path $PSScriptRoot '..\cloudflare-worker'
$log = Join-Path $Dir '.uploaded'
$done = @{}
if (Test-Path $log) { Get-Content $log | ForEach-Object { $done[$_] = $true } }
$target = if ($Local) { '--local' } else { '--remote' }
# 本地模拟桶是 SQLite, 并发写会撞锁
if ($Local) { $Parallel = 1 }

$files = foreach ($algo in 'md5', 'sha256') {
	Get-ChildItem (Join-Path $Dir $algo) -Filter '*.bin' | ForEach-Object {
		[pscustomobject]@{ Key = "$algo/$($_.Name)"; Path = $_.FullName; Size = $_.Length }
	}
}
$todo = @($files | Where-Object { -not $done.ContainsKey($_.Key) })
$todoBytes = ($todo | Measure-Object Size -Sum).Sum
Write-Host ("共 {0} 个, 已传 {1} 个, 待传 {2} 个 ({3:N1} GB)" -f $files.Count, ($files.Count - $todo.Count), $todo.Count, ($todoBytes / 1e9))
if ($todo.Count -eq 0) { return }

if ($LimitMBps -gt 0) {
	$admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
	if (-not $admin) { throw '限速要建 QoS 策略, 需要以管理员身份运行' }
	Remove-NetQosPolicy -Name $qosName -PolicyStore ActiveStore -Confirm:$false -ErrorAction SilentlyContinue
	New-NetQosPolicy -Name $qosName -AppPathNameMatchCondition 'node.exe' -ThrottleRateActionBitsPerSecond ([uint64]($LimitMBps * 8e6)) -PolicyStore ActiveStore -Confirm:$false | Out-Null
	Write-Host ("已限速 {0} MB/s (node.exe 上行), 并发 {1}" -f $LimitMBps, $Parallel)
}

$state = [hashtable]::Synchronized(@{
	Done = 0; Bytes = 0; Base = $files.Count - $todo.Count; Total = $files.Count
	Clock = [Diagnostics.Stopwatch]::StartNew()
})
$lock = [System.Threading.Mutex]::new($false)
try {
	$todo | ForEach-Object -ThrottleLimit $Parallel -Parallel {
		$f = $_
		$st = $using:state
		$limit = $using:LimitMBps
		# 兜底节奏: 本轮均速超过上限就先等, 不依赖 QoS 是按连接还是按合计限速
		if ($limit -gt 0) {
			$wait = $st.Bytes / ($limit * 1e6) - $st.Clock.Elapsed.TotalSeconds
			if ($wait -gt 0) { Start-Sleep -Milliseconds ([int]($wait * 1000)) }
		}
		Push-Location $using:worker
		try {
			$out = npx wrangler r2 object put "$($using:Bucket)/$($f.Key)" --file $f.Path --content-type application/octet-stream $using:target 2>&1
			if ($LASTEXITCODE -ne 0) { throw "上传失败: $($f.Key)`n$(($out | Select-Object -Last 8) -join "`n")" }
		} finally { Pop-Location }

		$m = $using:lock
		[void]$m.WaitOne()
		try {
			Add-Content -Path $using:log -Value $f.Key
			$st.Done++
			$st.Bytes += $f.Size
			$secs = $st.Clock.Elapsed.TotalSeconds
			$speed = $st.Bytes / 1e6 / $secs
			$left = [TimeSpan]::FromSeconds(($using:todoBytes - $st.Bytes) / 1e6 / $speed)
			$n = $st.Base + $st.Done
			$line = '[{0}/{1}] {2,-14} 均速 {3:N1} MB/s  已用 {4:hh\:mm\:ss}  剩余约 {5:hh\:mm\:ss}' -f $n, $st.Total, $f.Key, $speed, $st.Clock.Elapsed, $left
			Write-Host $line
			try { $Host.UI.RawUI.WindowTitle = ('R2 上传 {0}/{1} {2:N1}MB/s 剩 {3:hh\:mm}' -f $n, $st.Total, $speed, $left) } catch {}
		} finally { $m.ReleaseMutex() }
	}
	Write-Host '全部传完'
} finally {
	if ($LimitMBps -gt 0) {
		Remove-NetQosPolicy -Name $qosName -PolicyStore ActiveStore -Confirm:$false -ErrorAction SilentlyContinue
		Write-Host '已解除限速'
	}
}
