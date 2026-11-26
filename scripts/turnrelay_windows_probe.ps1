param(
    [Parameter(Mandatory)][string]$Core,
    [Parameter(Mandatory)][string]$VlessOutbound,
    [Parameter(Mandatory)][string]$TurnRelayOutbound,
    [uri]$Url = 'https://www.gstatic.com/generate_204',
    [ValidateRange(1, 20)][int]$Attempts = 3,
    [ValidateRange(3, 120)][int]$TimeoutSeconds = 15
)

$ErrorActionPreference = 'Stop'
$Core = (Resolve-Path -LiteralPath $Core).Path
$curl = (Get-Command curl.exe -CommandType Application).Source
$vlessJson = Get-Content -Raw -LiteralPath $VlessOutbound
$relay = Get-Content -Raw -LiteralPath $TurnRelayOutbound | ConvertFrom-Json
$original = $vlessJson | ConvertFrom-Json
if ($Url.Scheme -ne 'https' -or $original.type -ne 'vless' -or $relay.type -ne 'turnrelay') {
    throw 'Supply an HTTPS URL and separate VLESS and TurnRelay outbound JSON objects.'
}
if ($original.detour -or $relay.detour) {
    throw 'Supply standalone outbounds. This probe supplies the detour for the second path.'
}
$relay | Add-Member -Force NoteProperty tag 'probe-turnrelay'

function Invoke-PathProbe([string]$mode, [int]$attempt) {
    $vless = $vlessJson | ConvertFrom-Json
    $vless | Add-Member -Force NoteProperty tag 'probe-vless'
    $outbounds = @($vless)
    if ($mode -eq 'turnrelay') {
        $vless | Add-Member -Force NoteProperty detour $relay.tag
        $outbounds += $relay
    }
    $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
    $listener.Start()
    try { $port = $listener.LocalEndpoint.Port } finally { $listener.Stop() }
    $config = @{
        log = @{ level = 'warn' }
        inbounds = @(@{ type = 'mixed'; listen = '127.0.0.1'; listen_port = $port })
        outbounds = $outbounds
        route = @{ final = $vless.tag }
    }
    $info = [Diagnostics.ProcessStartInfo]::new($Core, 'run -c stdin')
    $info.UseShellExecute = $false
    $info.CreateNoWindow = $true
    $info.RedirectStandardInput = $true
    $p = [Diagnostics.Process]::Start($info)
    try {
        $bytes = [Text.Encoding]::UTF8.GetBytes(($config | ConvertTo-Json -Depth 64))
        $inputStream = $p.StandardInput.BaseStream
        $inputStream.Write($bytes, 0, $bytes.Length)
        $inputStream.Close()
        $startup = [Diagnostics.Stopwatch]::StartNew()
        while ($true) {
            if ($p.HasExited) { throw "Core exited during $mode startup (exit $($p.ExitCode))." }
            $socket = [Net.Sockets.TcpClient]::new()
            try { $socket.Connect('127.0.0.1', $port); break } catch [Net.Sockets.SocketException] {
                if ($startup.Elapsed.TotalSeconds -ge $TimeoutSeconds) { throw "Core startup timed out for $mode." }
            } finally { $socket.Dispose() }
            Start-Sleep -Milliseconds 100
        }
        $result = & $curl -q --silent --proxy "socks5h://127.0.0.1:$port" --max-time $TimeoutSeconds --output NUL --write-out '%{http_code} %{time_total}' $Url.AbsoluteUri
        $code = $LASTEXITCODE
        $parts = "$result" -split ' '
        [pscustomobject]@{ attempt = $attempt; path = $mode; curl_exit = $code; http = $parts[0]; seconds = $parts[1] } | ConvertTo-Json -Compress
    } finally {
        if (!$p.HasExited) { $p.Kill() }
        $p.WaitForExit()
        $p.Dispose()
    }
}

$noProxy = $env:NO_PROXY
try {
    $env:NO_PROXY = $null
    for ($i = 1; $i -le $Attempts; $i++) {
        $order = if ($i % 2) { 'direct', 'turnrelay' } else { 'turnrelay', 'direct' }
        foreach ($mode in $order) { Invoke-PathProbe $mode $i }
    }
} finally {
    $env:NO_PROXY = $noProxy
}
