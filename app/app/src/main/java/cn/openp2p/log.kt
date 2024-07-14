package cn.openp2p
import android.content.*
import java.io.File
import java.io.FileWriter
import java.text.SimpleDateFormat
import java.util.*
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

object Logger {
    private val logFile: File = File("app.log")

    fun log(message: String) {
        val timestamp = SimpleDateFormat("yyyy-MM-dd HH:mm:ss", Locale.getDefault()).format(Date())
        val logMessage = "$timestamp: $message\n"

        try {
            val fileWriter = FileWriter(logFile, true)
            fileWriter.append(logMessage)
            fileWriter.close()
        } catch (e: Exception) {
            e.printStackTrace()
        }
    }
}
