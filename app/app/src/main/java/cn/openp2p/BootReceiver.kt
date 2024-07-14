package cn.openp2p

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.util.Log
import cn.openp2p.ui.login.LoginActivity
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
//        Logger.log("pp onReceive "+intent.action.toString())
        Log.i("onReceive","start "+intent.action.toString())
        // if (Intent.ACTION_BOOT_COMPLETED == intent.action) {
        //     Log.i("onReceive","match "+intent.action.toString())
        //     VpnService.prepare(context)
        //     val intent = Intent(context, OpenP2PService::class.java)
        //     if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
        //         context.startForegroundService(intent)
        //     } else {
        //         context.startService(intent)
        //     }
        // }
        Log.i("onReceive","end "+intent.action.toString())
    }
}