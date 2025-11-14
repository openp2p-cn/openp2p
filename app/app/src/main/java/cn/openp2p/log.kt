package cn.openp2p
import android.util.Log
import java.text.SimpleDateFormat
import java.io.BufferedWriter
import java.io.File
import java.io.FileWriter
import java.io.IOException
import java.util.Date
import java.util.Locale

object Logger {
    private const val LOG_TAG = "OpenP2PLogger"
    private var logFile: File? = null
    private var bufferedWriter: BufferedWriter? = null

    // 初始化日志文件
    fun init(logDir: File, logFileName: String = "app.log") {
        if (!logDir.exists()) logDir.mkdirs()
        logFile = File(logDir, logFileName)

        try {
            bufferedWriter = BufferedWriter(FileWriter(logFile, true))
        } catch (e: IOException) {
            Log.e(LOG_TAG, "Failed to initialize BufferedWriter: ${e.message}")
        }
    }

    // 写日志（线程安全）
    @Synchronized
    fun log(level: String, tag: String, message: String, throwable: Throwable? = null) {
        val timestamp = SimpleDateFormat("yyyy-MM-dd HH:mm:ss", Locale.getDefault()).format(Date())
        val logMessage = "$timestamp $level $tag: $message"

        // 打印到 console
        when (level) {
            "ERROR" -> Log.e(tag, message, throwable)
            "WARN" -> Log.w(tag, message, throwable)
            "INFO" -> Log.i(tag, message)
            "DEBUG" -> Log.d(tag, message)
            "VERBOSE" -> Log.v(tag, message)
        }

        // 写入文件
        try {
            bufferedWriter?.apply {
                write(logMessage)
                newLine()
                flush()
            }
            throwable?.let {
                bufferedWriter?.apply {
                    write(Log.getStackTraceString(it))
                    newLine()
                    flush()
                }
            }
        } catch (e: IOException) {
            Log.e(LOG_TAG, "Failed to write log to file: ${e.message}")
        }
    }

    // 清理资源
    fun close() {
        try {
            bufferedWriter?.close()
        } catch (e: IOException) {
            Log.e(LOG_TAG, "Failed to close BufferedWriter: ${e.message}")
        }
    }

    // 简化方法
    fun e(tag: String, message: String, throwable: Throwable? = null) {
        log("ERROR", tag, message, throwable)
    }

    fun w(tag: String, message: String, throwable: Throwable? = null) {
        log("WARN", tag, message, throwable)
    }

    fun i(tag: String, message: String) {
        log("INFO", tag, message)
    }

    fun d(tag: String, message: String) {
        log("DEBUG", tag, message)
    }

    fun v(tag: String, message: String) {
        log("VERBOSE", tag, message)
    }
}
