package cn.openp2p

import android.app.Service
import android.content.Intent
import android.net.VpnService
import android.os.Binder
import android.os.IBinder
import openp2p.Openp2p
import android.util.Log
import cn.openp2p.ui.login.LoginActivity
import kotlinx.coroutines.GlobalScope
import kotlinx.coroutines.launch

class OpenP2PService : VpnService() {
    companion object {
        private val LOG_TAG = OpenP2PService::class.simpleName
    }

    inner class LocalBinder : Binder() {
        fun getService(): OpenP2PService = this@OpenP2PService
    }

    private val binder = LocalBinder()
    private lateinit var  network: openp2p.P2PNetwork
    override fun onCreate() {
        Log.i("xiao","onCreate - Thread ID = " + Thread.currentThread().id)
        super.onCreate()

    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        Log.i("xiao", "onStartCommand - startId = " + startId + ", Thread ID = " + Thread.currentThread().id)
        return super.onStartCommand(intent, flags, startId)
    }

    override fun onBind(p0: Intent?): IBinder? {
        return binder
    }

    override fun onDestroy() {
        Log.i("xiao", "onDestroy - Thread ID = " + Thread.currentThread().id)
        super.onDestroy()
    }
    fun onStart(token:String){
        GlobalScope.launch {
            do {
                network =Openp2p.runAsModule(getExternalFilesDir(null).toString(), token, 0)
                val isConnect = network.connect(30000)  // ms
                Log.i(OpenP2PService.LOG_TAG, "login result: " + isConnect.toString());
            } while(!network?.connect(10000))

        }
    }
    fun isConnected(): Boolean{
        if (!::network.isInitialized) return false
        return network?.connect(1000)
    }

}