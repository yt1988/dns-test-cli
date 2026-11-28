package cn.dnstest.android

import android.content.Context
import android.content.Intent
import android.net.ConnectivityManager
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Slider
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject
import java.util.Locale

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            MaterialTheme {
                Surface(
                    modifier = Modifier.fillMaxSize(),
                    color = MaterialTheme.colorScheme.background
                ) {
                    DnsTestScreen()
                }
            }
        }
    }
}

private class NativeDns {
    external fun run(args: String, systemDns: String): String

    companion object {
        init {
            System.loadLibrary("dnstest")
            System.loadLibrary("dnstest_jni")
        }
    }
}

private data class DnsResult(
    val resolver: String,
    val protocol: String,
    val address: String,
    val up: Boolean,
    val medianMs: Double?,
    val noAnswerRate: Double,
    val score: Double?,
    val error: String?
)

private val regionOptions = listOf(
    "" to "全部",
    "cn" to "中国大陆",
    "global" to "全球",
    "region" to "其他地区"
)

private val protocolOptions = listOf(
    "udp" to "UDP",
    "tcp" to "TCP",
    "dot" to "DoT",
    "doh" to "DoH",
    "doq" to "DoQ"
)

@Composable
private fun DnsTestScreen() {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val selectedProtocols = remember { mutableStateListOf("udp", "tcp", "dot", "doh", "doq") }

    var selectedRegion by remember { mutableStateOf("cn") }
    var rounds by remember { mutableStateOf(10f) }
    var useIpv6 by remember { mutableStateOf(false) }
    var offline by remember { mutableStateOf(false) }
    var running by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var latestRaw by remember { mutableStateOf("") }
    var results by remember { mutableStateOf<List<DnsResult>>(emptyList()) }

    val best = results.filter { it.up && it.score != null }.minByOrNull { it.score ?: Double.MAX_VALUE }
    val canStart = !running && selectedProtocols.isNotEmpty()

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 18.dp, vertical = 20.dp),
        verticalArrangement = Arrangement.spacedBy(14.dp)
    ) {
        Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(
                text = "DNS 测试",
                style = MaterialTheme.typography.headlineMedium,
                fontWeight = FontWeight.Bold
            )
            Text(
                text = "在当前手机网络中实测公共 DNS，结果不会上传。",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant
            )
        }

        Card(
            modifier = Modifier.fillMaxWidth(),
            colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainer)
        ) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text("测试地区", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
                Row(
                    modifier = Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),
                    horizontalArrangement = Arrangement.spacedBy(8.dp)
                ) {
                    regionOptions.forEach { option ->
                        FilterChip(
                            selected = selectedRegion == option.first,
                            onClick = { selectedRegion = option.first },
                            label = { Text(option.second) }
                        )
                    }
                }
            }
        }

        Card(
            modifier = Modifier.fillMaxWidth(),
            colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainer)
        ) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text("测试协议", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
                Row(
                    modifier = Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),
                    horizontalArrangement = Arrangement.spacedBy(8.dp)
                ) {
                    protocolOptions.forEach { option ->
                        FilterChip(
                            selected = option.first in selectedProtocols,
                            onClick = {
                                if (option.first in selectedProtocols) {
                                    selectedProtocols.remove(option.first)
                                } else {
                                    selectedProtocols.add(option.first)
                                }
                            },
                            label = { Text(option.second) }
                        )
                    }
                }
                Text(
                    text = "支持 UDP、TCP、DNS over TLS、DNS over HTTPS 和 DNS over QUIC。",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
            }
        }

        Card(
            modifier = Modifier.fillMaxWidth(),
            colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainer)
        ) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text("测试轮数", modifier = Modifier.weight(1f), fontWeight = FontWeight.SemiBold)
                    Text("${rounds.toInt()} 轮", color = MaterialTheme.colorScheme.primary)
                }
                Slider(
                    value = rounds,
                    onValueChange = { rounds = it },
                    valueRange = 2f..20f,
                    steps = 17
                )
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        Text("使用 IPv6")
                        Text("通过 IPv6 网络地址进行测试", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    Switch(checked = useIpv6, onCheckedChange = { useIpv6 = it })
                }
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        Text("离线模式")
                        Text("使用应用内置 DNS 列表，不获取在线更新", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    Switch(checked = offline, onCheckedChange = { offline = it })
                }
            }
        }

        Button(
            onClick = {
                running = true
                error = null
                results = emptyList()
                val args = buildList {
                    if (selectedRegion.isNotBlank()) addAll(listOf("-g", selectedRegion))
                    if (selectedProtocols.size != protocolOptions.size) {
                        addAll(listOf("-p", selectedProtocols.joinToString(",")))
                    }
                    addAll(listOf("-n", rounds.toInt().toString(), "-C", "8", "-t", "2s"))
                    add(if (useIpv6) "-6" else "-4")
                    if (offline) add("--offline")
                }.joinToString(" ")

                scope.launch {
                    try {
                        val dns = getAndroidDnsServers(context)
                        val raw = withContext(Dispatchers.IO) {
                            NativeDns().run(args, dns)
                        }
                        latestRaw = raw
                        val parsed = parseResults(raw)
                        results = parsed.first
                        error = parsed.second
                        if (results.isEmpty() && error == null) {
                            error = "没有获得可用结果，请检查当前网络或更换测试范围。"
                        }
                    } catch (e: Exception) {
                        error = e.message ?: "测试失败，请稍后重试。"
                    } finally {
                        running = false
                    }
                }
            },
            enabled = canStart,
            modifier = Modifier.fillMaxWidth()
        ) {
            Text(if (running) "正在测试…" else "开始 DNS 测试")
        }

        if (running) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.Center,
                verticalAlignment = Alignment.CenterVertically
            ) {
                CircularProgressIndicator()
                Spacer(Modifier.width(12.dp))
                Text("正在逐项测量 DNS 响应时间，请保持网络连接。")
            }
        }

        error?.let {
            Card(
                modifier = Modifier.fillMaxWidth(),
                colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.errorContainer)
            ) {
                Text(it, modifier = Modifier.padding(14.dp), color = MaterialTheme.colorScheme.onErrorContainer)
            }
        }

        if (best != null) {
            Card(
                modifier = Modifier.fillMaxWidth(),
                colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.primaryContainer)
            ) {
                Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Text("当前网络最快", style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onPrimaryContainer)
                    Text(best.resolver, style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold, color = MaterialTheme.colorScheme.onPrimaryContainer)
                    Text(
                        "${best.protocol.uppercase(Locale.ROOT)} · ${formatMs(best.medianMs)}",
                        style = MaterialTheme.typography.titleMedium,
                        color = MaterialTheme.colorScheme.onPrimaryContainer
                    )
                }
            }
        }

        if (results.isNotEmpty()) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text("测试结果", style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold, modifier = Modifier.weight(1f))
                OutlinedButton(onClick = {
                    val send = Intent(Intent.ACTION_SEND).apply {
                        type = "application/json"
                        putExtra(Intent.EXTRA_SUBJECT, "DNS 测试结果")
                        putExtra(Intent.EXTRA_TEXT, latestRaw)
                    }
                    context.startActivity(Intent.createChooser(send, "分享测试结果"))
                }) {
                    Text("分享 JSON")
                }
            }

            results.take(60).forEach { item ->
                Card(
                    modifier = Modifier.fillMaxWidth(),
                    colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow)
                ) {
                    Row(
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 14.dp, vertical = 12.dp),
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                            Text(item.resolver, fontWeight = FontWeight.SemiBold)
                            Text(
                                "${item.protocol.uppercase(Locale.ROOT)} · ${item.address}",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant
                            )
                            if (!item.up && !item.error.isNullOrBlank()) {
                                Text(item.error, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
                            }
                        }
                        Column(horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(3.dp)) {
                            Text(
                                if (item.up) formatMs(item.medianMs) else "无应答",
                                fontWeight = FontWeight.Bold,
                                color = if (item.up) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.error
                            )
                            Text(
                                "无应答率 ${(item.noAnswerRate * 100).toInt()}%",
                                style = MaterialTheme.typography.labelSmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant
                            )
                        }
                    }
                }
            }
        }

        Text(
            text = "说明：测试流量直接从本机发出；系统 DNS 地址取自 Android 当前网络信息。测速结果会受 Wi-Fi、移动网络、VPN 和运营商线路影响。",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant
        )
    }
}

private fun getAndroidDnsServers(context: Context): String {
    val manager = context.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
    val network = manager.activeNetwork ?: return ""
    val properties = manager.getLinkProperties(network) ?: return ""
    return properties.dnsServers
        .mapNotNull { it.hostAddress?.substringBefore("%") }
        .distinct()
        .joinToString(",")
}

private fun parseResults(raw: String): Pair<List<DnsResult>, String?> {
    val root = JSONObject(raw)
    val error = root.optString("error").takeIf { it.isNotBlank() && it != "null" }
    if (error != null) return emptyList<DnsResult>() to error

    val array: JSONArray = root.optJSONArray("results") ?: JSONArray()
    val rows = buildList {
        for (i in 0 until array.length()) {
            val item = array.optJSONObject(i) ?: continue
            val median = if (item.isNull("median_ms")) null else item.optDouble("median_ms")
            val score = if (item.isNull("score")) null else item.optDouble("score")
            add(
                DnsResult(
                    resolver = item.optString("name", item.optString("resolver")),
                    protocol = item.optString("proto"),
                    address = item.optString("address"),
                    up = item.optBoolean("up"),
                    medianMs = median,
                    noAnswerRate = item.optDouble("loss", 0.0),
                    score = score,
                    error = item.optString("error").takeIf { it.isNotBlank() && it != "null" }
                )
            )
        }
    }
    return rows to null
}

private fun formatMs(value: Double?): String =
    value?.let { String.format(Locale.US, "%.2f ms", it) } ?: "—"
