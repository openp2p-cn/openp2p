package cn.openp2p

import android.app.*
import android.content.Context
import android.content.Intent
import android.graphics.Color
import android.net.VpnService
import android.os.Binder
import android.os.Build
import android.os.IBinder
import android.os.ParcelFileDescriptor
import android.util.Log
import androidx.annotation.RequiresApi
import androidx.core.app.NotificationCompat
import cn.openp2p.ui.login.LoginActivity
import kotlinx.coroutines.GlobalScope
import kotlinx.coroutines.launch
import openp2p.Openp2p
import java.io.FileInputStream
import java.io.FileOutputStream
import java.nio.ByteBuffer
import kotlinx.coroutines.*
import org.json.JSONObject
import java.io.File
import java.net.InetAddress
import java.net.NetworkInterface
import kotlinx.coroutines.channels.Channel
import java.nio.channels.FileChannel
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

data class Node(val name: String, val ip: String, val resource: String? = null)


data class Network(
    val id: Long,
    val name: String,
    val gateway: String,
    val Nodes: List<Node>
)

class OpenP2PService : VpnService() {
    companion object {
        private val LOG_TAG = "OpenP2PService"
    }

    inner class LocalBinder : Binder() {
        fun getService(): OpenP2PService = this@OpenP2PService
    }

    private val binder = LocalBinder()
    private lateinit var network: openp2p.P2PNetwork
    private lateinit var mToken: String
    private var running: Boolean = true
    private var sdwanRunning: Boolean = false
    private var vpnInterface: ParcelFileDescriptor? = null
    private var sdwanJob: Job? = null
    private val packetQueue = Channel<ByteBuffer>(capacity = 1024)
    private val serviceScope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    override fun onCreate() {
        val logDir = File(getExternalFilesDir(null), "log")
        Logger.init(logDir)
        Logger.i(LOG_TAG, "onCreate - Thread ID = " + Thread.currentThread().id)
        var channelId = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            createNotificationChannel("kim.hsl", "ForegroundService")
        } else {
            ""
        }
        val notificationIntent = Intent(this, LoginActivity::class.java)

        val pendingIntent = PendingIntent.getActivity(
            this, 0,
            notificationIntent, PendingIntent.FLAG_IMMUTABLE
        )

        val notification = channelId?.let {
            NotificationCompat.Builder(this, it)
                //            .setSmallIcon(R.mipmap.app_icon)
                .setContentTitle("My Awesome App")
                .setContentText("Doing some work...")
                .setContentIntent(pendingIntent).build()
        }

        startForeground(1337, notification)
        super.onCreate()
        refreshSDWAN()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        Logger.i(
            LOG_TAG,
            "onStartCommand - startId = " + startId + ", Thread ID = " + Thread.currentThread().id
        )
        startOpenP2P(null)
        return super.onStartCommand(intent, flags, startId)
    }

    override fun onBind(p0: Intent?): IBinder? {
        val token = p0?.getStringExtra("token")
        Logger.i(LOG_TAG, "onBind token=$token")
        startOpenP2P(token)
        return binder
    }

    private fun startOpenP2P(token: String?): Boolean {
        if (sdwanRunning) {
            return true
        }

        Logger.i(LOG_TAG, "startOpenP2P - Thread ID = " + Thread.currentThread().id + token)
        val oldToken = Openp2p.getToken(getExternalFilesDir(null).toString())
        Logger.i(LOG_TAG, "startOpenP2P oldtoken=$oldToken newtoken=$token")
        if (oldToken == "0" && token == null) {
            return false
        }
        sdwanRunning = true
        //        runSDWAN()
        GlobalScope.launch {
            network = Openp2p.runAsModule(
                getExternalFilesDir(null).toString(),
                token,
                0,
                1
            ) // /storage/emulated/0/Android/data/cn.openp2p/files/
            val isConnect = network.connect(30000)  // ms
            Logger.i(LOG_TAG, "login result: " + isConnect.toString());
            do {
                Thread.sleep(1000)
            } while (network.connect(30000) && running)
            stopSelf()
        }
        return false
    }

    private fun refreshSDWAN() {
        GlobalScope.launch {
            Logger.i(OpenP2PService.LOG_TAG, "refreshSDWAN start");
            while (true) {
                Logger.i(OpenP2PService.LOG_TAG, "waiting new sdwan config");
                val buf = ByteArray(32 * 1024)
                val buffLen = Openp2p.getAndroidSDWANConfig(buf)
                Logger.i(OpenP2PService.LOG_TAG, "closing running sdwan instance");
                sdwanRunning = false
                vpnInterface?.close()
                vpnInterface = null
                sdwanJob?.join()
                sdwanJob = serviceScope.launch(context = Dispatchers.IO) {
                    runSDWAN(buf.copyOfRange(0, buffLen.toInt()))
                }
            }
            Logger.i(OpenP2PService.LOG_TAG, "refreshSDWAN end");
        }
    }

    private suspend fun readTunLoop() {
        val inputStream = FileInputStream(vpnInterface?.fileDescriptor).channel
        if (inputStream == null) {
            Logger.i(OpenP2PService.LOG_TAG, "open FileInputStream error: ");
            return
        }
        Logger.i(LOG_TAG, "read tun loop start")
        val buffer = ByteBuffer.allocate(4096)
        val byteArrayRead = ByteArray(4096)
        while (sdwanRunning) {
            buffer.clear()
            withContext(Dispatchers.IO) {
                val readBytes = inputStream.read(buffer)
                if (readBytes > 0) {
                    buffer.flip()
                    buffer.get(byteArrayRead, 0, readBytes)
                    // Logger.i(OpenP2PService.LOG_TAG, String.format("Openp2p.androidRead: %d", readBytes))
                    Openp2p.androidRead(byteArrayRead, readBytes.toLong())
                    // Logger.i(OpenP2PService.LOG_TAG, "inputStream.read error: ")
                } else {
                    delay(50)
                }
            }
        }
        Logger.i(LOG_TAG, "read tun loop end")
    }


    private suspend fun runSDWAN(buf: ByteArray) {
//        val localIps = listOf(
//            "fe80::14b6:a0ff:fe3e:64de" to 64,
//            "192.168.100.184" to 24,
//            "10.93.158.91" to 32,
//            "192.168.3.66" to 24
//        )
//
//        // 测试用例
//        val testCases = listOf(
//            "192.168.3.11" to true,
//            "192.168.100.1" to true,
//            "192.168.101.1" to false,
//            "10.93.158.91" to true,
//            "10.93.158.90" to false,
//            "fe80::14b6:a0ff:fe3e:64de" to true,
//            "fe80::14b6:a0ff:fe3e:64dd" to true // 在同一子网
//        )
//
//        for ((ip, expected) in testCases) {
//            val result = isSameSubnet(ip, localIps)
//            println("Testing IP: $ip, Expected: $expected, Result: $result")
//        }
        sdwanRunning = true

        Logger.i(OpenP2PService.LOG_TAG, "runSDWAN start:${buf.decodeToString()}");
        try {
            var builder = Builder()
           val jsonObject = JSONObject(buf.decodeToString())
            // debug sdwan info
            // val jsonObject = JSONObject("""{"id":2817104318517097000,"name":"network1","gateway":"10.2.3.254/24","mode":"central","centralNode":"nanjin-192-168-0-82","enable":1,"tunnelNum":3,"mtu":1420,"Nodes":[{"name":"192-168-24-15","ip":"10.2.3.5"},{"name":"Alpine Linux-172.16","ip":"10.2.3.14","resource":"172.16.0.0/24"},{"name":"ctdeMacBook-Pro.local","ip":"10.2.3.22"},{"name":"dengjiandeMBP.sh.chaitin.net","ip":"10.2.3.32"},{"name":"DESKTOP-WIN11-ARM-self","ip":"10.2.3.19"},{"name":"eastdeMBP.sh.chaitin.net","ip":"10.2.3.3"},{"name":"FN-NAS-HP","ip":"10.2.3.1","resource":"192.168.100.0/24"},{"name":"huangruideMBP.sh.chaitin.net","ip":"10.2.3.30"},{"name":"iStoreOS-virtual-machine","ip":"10.2.3.12"},{"name":"k30s-redmi-10.2.33","ip":"10.2.3.27"},{"name":"lincheng-MacBook-Pro-3.sh.chaitin.net","ip":"10.2.3.15"},{"name":"localhost-mi-13","ip":"10.2.3.8"},{"name":"localhost-华为matepad11","ip":"10.2.3.13"},{"name":"luzhanwendeMacBook-Pro.local","ip":"10.2.3.17"},{"name":"Mi-pad2-local","ip":"10.2.3.9"},{"name":"nanjin-192-168-0-82","ip":"10.2.3.34"},{"name":"R7000P-2021","ip":"10.2.3.7"},{"name":"tanxiaolongsMBP.sh.chaitin.net","ip":"10.2.3.20"},{"name":"TUF-AX3000_V2-3804","ip":"10.2.3.25"},{"name":"WIN-CYZ-10.2.3.16","ip":"10.2.3.16"},{"name":"WODOUYAO","ip":"10.2.3.4"},{"name":"Zstrack01","ip":"10.2.3.51","resource":"192.168.24.0/22,192.168.20.0/24"},{"name":"小米14-localhost","ip":"10.2.3.23"}]}""")
            val id = jsonObject.getLong("id")
            val mtu = jsonObject.getInt("mtu")
            val name = jsonObject.getString("name")
            val gateway = jsonObject.getString("gateway")
            val nodesArray = jsonObject.getJSONArray("Nodes")

            val nodesList = mutableListOf<JSONObject>()
            for (i in 0 until nodesArray.length()) {
                nodesList.add(nodesArray.getJSONObject(i))
            }

            val myNodeName = Openp2p.getAndroidNodeName()
            // 使用本地 IP 和子网判断是否需要添加路由
            val localIps = getLocalIpAndSubnet()
            Logger.i(OpenP2PService.LOG_TAG, "getAndroidNodeName:${myNodeName}");
            val nodeList = nodesList.map {
                val nodeName = it.getString("name")
                val nodeIp = it.getString("ip")
                if (nodeName == myNodeName) {
                    try {
                        Logger.i(LOG_TAG, "Attempting to add address: $nodeIp/24")
                        builder.addAddress(nodeIp, 24)
                        Logger.i(LOG_TAG, "Successfully added address")
                    } catch (e: Exception) {
                        Logger.e(LOG_TAG, "Failed to add address $nodeIp: ${e.message}")
                        throw e // or handle gracefully
                    }
                }
                val nodeResource = it.optString("resource", null)
                if (!nodeResource.isNullOrEmpty()) {
                    // 可能是多个网段，用逗号分隔
                    val resourceList = nodeResource.split(",")
                    for (resource in resourceList) {
                        val parts = resource.split("/")
                        if (parts.size == 2) {
                            val ipAddress = parts[0].trim()
                            val subnetMask = parts[1].trim()
                            // 判断是否属于本机网段
                            if (!isSameSubnet(ipAddress, localIps)) {
                                builder.addRoute(ipAddress, subnetMask.toInt())
                                Logger.i(
                                    OpenP2PService.LOG_TAG,
                                    "sdwan addRoute:${ipAddress},${subnetMask}"
                                )
                            } else {
                                Logger.i(
                                    OpenP2PService.LOG_TAG,
                                    "Skipped adding route for ${ipAddress}, already in local subnet"
                                )
                            }
                        } else {
                            Logger.w(OpenP2PService.LOG_TAG, "Invalid resource format: $resource")
                        }
                    }
                }

                Node(nodeName, nodeIp, nodeResource)
            }

            val network = Network(id, name, gateway, nodeList)
            println(network)
            Logger.i(OpenP2PService.LOG_TAG, "onBind");
            builder.addDnsServer("119.29.29.29")
            builder.addDnsServer("2400:3200::1") // alicloud dns v6 & v4
            // builder.addRoute("10.2.3.0", 24)
//            builder.addRoute("0.0.0.0", 0);
            val gatewayStr = jsonObject.optString("gateway", "")
            val subNet = getNetworkAddress(gatewayStr)
            if (subNet != null) {
                val (netIp, prefix) = subNet
                builder.addRoute(netIp, prefix)
                Logger.i(OpenP2PService.LOG_TAG, "Added route from gateway: $netIp/$prefix")
            } else {
                Logger.w(OpenP2PService.LOG_TAG, "Invalid gateway format: $gatewayStr")
            }

            builder.setSession(LOG_TAG!!)
            builder.setMtu(mtu)
            vpnInterface = builder.establish()
            if (vpnInterface == null) {
                Log.e(OpenP2PService.LOG_TAG, "start vpnservice error: ");
            }

            val byteArrayWrite = ByteArray(4096)
            serviceScope.launch(Dispatchers.IO) {
                readTunLoop() // 文件读操作，适合 Dispatchers.IO
            }

            val outputStream = FileOutputStream(vpnInterface?.fileDescriptor).channel
            if (outputStream == null) {
                Log.e(OpenP2PService.LOG_TAG, "open FileOutputStream error: ");
                return
            }
            Logger.i(LOG_TAG, "write tun loop start")
            while (sdwanRunning) {
                val len = Openp2p.androidWrite(byteArrayWrite, 3000)
                if (len > mtu || len.toInt() == 0) {
                    continue
                }
                try {
                    val writeBytes =
                        outputStream?.write(ByteBuffer.wrap(byteArrayWrite, 0, len.toInt()))
                    if (writeBytes != null && writeBytes <= 0) {
                        Logger.e(LOG_TAG, "outputStream.write failed: $writeBytes")
                    }
                } catch (e: Exception) {
                    Logger.e(LOG_TAG, "outputStream.write exception: ${e.message}")
                    e.printStackTrace()
                    continue
                }
            }
            outputStream.close()
            vpnInterface?.close()
            vpnInterface = null
            Logger.i(LOG_TAG, "write tun loop end")
        } catch (e: Exception) {
            Logger.i("VPN Connection", "发生异常: ${e.message}")
        }
        Logger.i(OpenP2PService.LOG_TAG, "runSDWAN end");
    }
    /**
     * 将 "10.2.3.254/16" 这样的 CIDR 转成正确对齐的网络地址，如 "10.2.0.0/16"
     */
    fun getNetworkAddress(cidr: String): Pair<String, Int>? {
        val parts = cidr.trim().split("/")
        if (parts.size != 2) return null

        val ip = parts[0]
        val prefix = parts[1].toIntOrNull() ?: return null
        if (prefix !in 0..32) return null

        val octets = ip.split(".").map { it.toInt() }
        if (octets.size != 4) return null

        // 转成整数
        val ipInt = (octets[0] shl 24) or (octets[1] shl 16) or (octets[2] shl 8) or octets[3]

        // 生成掩码并计算网络地址
        val mask = if (prefix == 0) 0 else (-1 shl (32 - prefix))
        val networkInt = ipInt and mask

        // 转回点分十进制
        val networkIp = listOf(
            (networkInt shr 24) and 0xFF,
            (networkInt shr 16) and 0xFF,
            (networkInt shr 8) and 0xFF,
            networkInt and 0xFF
        ).joinToString(".")

        return networkIp to prefix
    }
    override fun onDestroy() {
        super.onDestroy()
        Logger.i(LOG_TAG, "onDestroy - Canceling service scope")
        serviceScope.cancel() // 取消所有与服务相关的协程
    }

    override fun onUnbind(intent: Intent?): Boolean {
        Logger.i(LOG_TAG, "onUnbind - Thread ID = " + Thread.currentThread().id)
        stopSelf()
        return super.onUnbind(intent)
    }

    fun isConnected(): Boolean {
        if (!::network.isInitialized) return false
        return network.connect(1000)
    }

    fun stop() {
        running = false
        stopSelf()
        Openp2p.stop()
    }

    @RequiresApi(Build.VERSION_CODES.O)
    private fun createNotificationChannel(channelId: String, channelName: String): String? {
        val chan = NotificationChannel(
            channelId,
            channelName, NotificationManager.IMPORTANCE_NONE
        )
        chan.lightColor = Color.BLUE
        chan.lockscreenVisibility = Notification.VISIBILITY_PRIVATE
        val service = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        service.createNotificationChannel(chan)
        return channelId
    }
}

// 获取本机所有IP地址和对应的子网信息
fun getLocalIpAndSubnet(): List<Pair<String, Int>> {
    val localIps = mutableListOf<Pair<String, Int>>()
    val networkInterfaces = NetworkInterface.getNetworkInterfaces()
    // 手动添加测试数据
    //localIps.add(Pair("192.168.3.33", 24))
    while (networkInterfaces.hasMoreElements()) {
        val networkInterface = networkInterfaces.nextElement()
        if (networkInterface.isUp && !networkInterface.isLoopback) {
            val interfaceAddresses = networkInterface.interfaceAddresses
            for (interfaceAddress in interfaceAddresses) {
                val address = interfaceAddress.address
                val prefixLength = interfaceAddress.networkPrefixLength
                if (address is InetAddress) {
                    localIps.add(Pair(address.hostAddress, prefixLength.toInt()))
                }
            }
        }
    }
    return localIps
}

// 判断某个IP是否与本机某网段匹配
fun isSameSubnet(ipAddress: String, localIps: List<Pair<String, Int>>): Boolean {
    val targetIp = InetAddress.getByName(ipAddress).address
    for ((localIp, prefixLength) in localIps) {
        val localIpBytes = InetAddress.getByName(localIp).address
        val mask = createSubnetMask(prefixLength, localIpBytes.size) // 动态生成掩码

        // 比较目标 IP 和本地 IP 的网络部分
        if (targetIp.indices.all { i ->
                (targetIp[i].toInt() and mask[i].toInt()) == (localIpBytes[i].toInt() and mask[i].toInt())
            }) {
            return true
        }
    }
    return false
}

// 根据前缀长度动态生成子网掩码
fun createSubnetMask(prefixLength: Int, addressLength: Int): ByteArray {
    val mask = ByteArray(addressLength)
    for (i in 0 until prefixLength / 8) {
        mask[i] = 0xFF.toByte()
    }
    if (prefixLength % 8 != 0) {
        mask[prefixLength / 8] = (0xFF shl (8 - (prefixLength % 8))).toByte()
    }
    return mask
}