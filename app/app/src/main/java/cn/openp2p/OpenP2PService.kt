package cn.openp2p

import android.app.*
import android.content.Context
import android.content.Intent
import android.graphics.Color
import android.os.Binder
import android.os.Build
import android.os.IBinder
import android.util.Log
import androidx.annotation.RequiresApi
import androidx.core.app.NotificationCompat
import cn.openp2p.ui.login.LoginActivity
import kotlinx.coroutines.GlobalScope
import kotlinx.coroutines.launch
import openp2p.Openp2p


class OpenP2PService : Service() {
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
    override fun onCreate() {
        Log.i(LOG_TAG, "onCreate - Thread ID = " + Thread.currentThread().id)
        var channelId: String? = null
        channelId = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            createNotificationChannel("kim.hsl", "ForegroundService")
        } else {
            ""
        }
        val notificationIntent = Intent(this, LoginActivity::class.java)

        val pendingIntent = PendingIntent.getActivity(
            this, 0,
            notificationIntent, 0
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

    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        Log.i(
            LOG_TAG,
            "onStartCommand - startId = " + startId + ", Thread ID = " + Thread.currentThread().id
        )
        return super.onStartCommand(intent, flags, startId)
    }

    override fun onBind(p0: Intent?): IBinder? {

        val token = p0?.getStringExtra("token")
        Log.i(LOG_TAG, "onBind - Thread ID = " + Thread.currentThread().id + token)
        GlobalScope.launch {
            network = Openp2p.runAsModule(getExternalFilesDir(null).toString(), token, 0, 1)
            val isConnect = network.connect(30000)  // ms
            Log.i(OpenP2PService.LOG_TAG, "login result: " + isConnect.toString());
            do {
                Thread.sleep(1000)
            }while(network.connect(30000)&&running)
            stopSelf()
        }
        return binder
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
        return network?.connect(1000)
    }

    fun stop() {
        running=false
        stopSelf()
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