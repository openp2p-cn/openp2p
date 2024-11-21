package cn.openp2p

import android.app.*
import android.content.Context
import android.content.Intent
import android.graphics.Color
import java.io.IOException
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

data class Node(val name: String, val ip: String, val resource: String? = null)


data class Network(
    val id: Long,
    val name: String,
    val gateway: String,
    val Nodes: List<Node>
)
class OpenP2PService : VpnService() {
    companion object {
        private val LOG_TAG = OpenP2PService::class.simpleName
    }

    inner class LocalBinder : Binder() {
        fun getService(): OpenP2PService = this@OpenP2PService
    }

    private val binder = LocalBinder()
    private lateinit var network: openp2p.P2PNetwork
    private lateinit var mToken: String
    private var running:Boolean =true
    private var sdwanRunning:Boolean =false
    private var vpnInterface: ParcelFileDescriptor? = null
    private  var sdwanJob: Job? = null
    override fun onCreate() {
        Log.i(LOG_TAG, "onCreate - Thread ID = " + Thread.currentThread().id)
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
        Log.i(
            LOG_TAG,
            "onStartCommand - startId = " + startId + ", Thread ID = " + Thread.currentThread().id
        )
        startOpenP2P(null)
        return super.onStartCommand(intent, flags, startId)
    }

    override fun onBind(p0: Intent?): IBinder? {
        val token = p0?.getStringExtra("token")
        Log.i(LOG_TAG, "onBind token=$token")
        startOpenP2P(token)
        return binder
    }

    private fun startOpenP2P(token : String?): Boolean {
        if (sdwanRunning) {
            return true
        }

        Log.i(LOG_TAG, "startOpenP2P - Thread ID = " + Thread.currentThread().id + token)
        val oldToken = Openp2p.getToken(getExternalFilesDir(null).toString())
        Log.i(LOG_TAG, "startOpenP2P oldtoken=$oldToken newtoken=$token")
        if (oldToken=="0" && token==null){
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
            Log.i(LOG_TAG, "login result: " + isConnect.toString());
            do {
                Thread.sleep(1000)
            } while (network.connect(30000) && running)
            stopSelf()
        }
        return false
    }

    private fun refreshSDWAN() {
        GlobalScope.launch {
            Log.i(OpenP2PService.LOG_TAG, "refreshSDWAN start");
            while (true) {
                Log.i(OpenP2PService.LOG_TAG, "waiting new sdwan config");
                val buf = ByteArray(4096)
                val buffLen = Openp2p.getAndroidSDWANConfig(buf)
                Log.i(OpenP2PService.LOG_TAG, "closing running sdwan instance");
                sdwanRunning = false
                vpnInterface?.close()
                vpnInterface = null
                Thread.sleep(10000)
                runSDWAN(buf.copyOfRange(0,buffLen.toInt() ))
            }
            Log.i(OpenP2PService.LOG_TAG, "refreshSDWAN end");
        }
    }
    private suspend fun readTunLoop() {
        val inputStream = FileInputStream(vpnInterface?.fileDescriptor).channel
        if (inputStream==null){
            Log.i(OpenP2PService.LOG_TAG, "open FileInputStream error: ");
            return
        }
        Log.d(LOG_TAG, "read tun loop start")
        val buffer = ByteBuffer.allocate(4096)
        val byteArrayRead = ByteArray(4096)
        while (sdwanRunning) {
            buffer.clear()
            val readBytes = inputStream.read(buffer)
            if (readBytes <= 0) {
//                Log.i(OpenP2PService.LOG_TAG, "inputStream.read error: ")
                delay(1)
                continue
            }
            buffer.flip()
            buffer.get(byteArrayRead,0,readBytes)
            Log.i(OpenP2PService.LOG_TAG, String.format("Openp2p.androidRead: %d", readBytes))
            Openp2p.androidRead(byteArrayRead, readBytes.toLong())

        }
        Log.d(LOG_TAG, "read tun loop end")
    }

    private fun runSDWAN(buf:ByteArray) {
        sdwanRunning=true
        sdwanJob=GlobalScope.launch(context = Dispatchers.IO) {
            Log.i(OpenP2PService.LOG_TAG, "runSDWAN start:${buf.decodeToString()}");
            try{
                var builder = Builder()
                val jsonObject = JSONObject(buf.decodeToString())
                val id = jsonObject.getLong("id")
                val name = jsonObject.getString("name")
                val gateway = jsonObject.getString("gateway")
                val nodesArray = jsonObject.getJSONArray("Nodes")

                val nodesList = mutableListOf<JSONObject>()
                for (i in 0 until nodesArray.length()) {
                    nodesList.add(nodesArray.getJSONObject(i))
                }

                val myNodeName = Openp2p.getAndroidNodeName()
                Log.i(OpenP2PService.LOG_TAG, "getAndroidNodeName:${myNodeName}");
                val nodeList = nodesList.map {
                    val nodeName = it.getString("name")
                    val nodeIp = it.getString("ip")
                    if (nodeName==myNodeName){
                        builder.addAddress(nodeIp, 24)
                    }
                    val nodeResource = if (it.has("resource")) it.getString("resource") else null
                    val parts = nodeResource?.split("/")
                    if (parts?.size == 2) {
                        val ipAddress = parts[0]
                        val subnetMask = parts[1]
                        builder.addRoute(ipAddress, subnetMask.toInt())
                        Log.i(OpenP2PService.LOG_TAG, "sdwan addRoute:${ipAddress},${subnetMask.toInt()}");
                    }
                    Node(nodeName, nodeIp, nodeResource)
                }

                val network = Network(id, name, gateway, nodeList)
                println(network)
                Log.i(OpenP2PService.LOG_TAG, "onBind");
                builder.addDnsServer("223.5.5.5")
                builder.addDnsServer("2400:3200::1") // alicloud dns v6 & v4
                builder.addRoute("10.2.3.0", 24)
//            builder.addRoute("0.0.0.0", 0);
                builder.setSession(LOG_TAG!!)
                builder.setMtu(1420)
                vpnInterface = builder.establish()
                if (vpnInterface==null){
                    Log.e(OpenP2PService.LOG_TAG, "start vpnservice error: ");
                }
               val outputStream = FileOutputStream(vpnInterface?.fileDescriptor).channel
               if (outputStream==null){
                   Log.e(OpenP2PService.LOG_TAG, "open FileOutputStream error: ");
                   return@launch
               }

               val byteArrayWrite = ByteArray(4096)
                launch {
                        readTunLoop()
                    }

                Log.d(LOG_TAG, "write tun loop start")
               while (sdwanRunning) {
                   val len = Openp2p.androidWrite(byteArrayWrite)
                   Log.i(OpenP2PService.LOG_TAG, String.format("Openp2p.androidWrite: %d",len));
                   val writeBytes = outputStream?.write(ByteBuffer.wrap(byteArrayWrite))
                   if (writeBytes != null && writeBytes <= 0) {
                       Log.i(OpenP2PService.LOG_TAG, "outputStream?.write error: ");
                       continue
                   }
               }
               outputStream.close()
// 关闭 VPN 接口
                vpnInterface?.close()
// 置空变量以释放资源
                vpnInterface = null
                Log.d(LOG_TAG, "write tun loop end")
            }catch (e: Exception) {
                // 捕获异常并记录
                Log.e("VPN Connection", "发生异常: ${e.message}")
            }
            Log.i(OpenP2PService.LOG_TAG, "runSDWAN end");
        }

    }

    override fun onDestroy() {
        Log.i(LOG_TAG, "onDestroy - Thread ID = " + Thread.currentThread().id)
        super.onDestroy()
    }

    override fun onUnbind(intent: Intent?): Boolean {
        Log.i(LOG_TAG, "onUnbind - Thread ID = " + Thread.currentThread().id)
        stopSelf()
        return super.onUnbind(intent)
    }
    fun isConnected(): Boolean {
        if (!::network.isInitialized) return false
        return network.connect(1000)
    }

    fun stop() {
        running=false
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