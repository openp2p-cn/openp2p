package cn.openp2p.ui.login

import android.annotation.SuppressLint
import android.app.Activity
import android.app.ActivityManager
import android.app.Notification
import android.app.PendingIntent
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.ServiceConnection
import android.net.Uri
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.os.IBinder
import android.text.Editable
import android.text.TextWatcher
import android.util.Log
import android.view.View
import android.widget.EditText
import android.widget.Toast
import androidx.annotation.RequiresApi
import androidx.annotation.StringRes
import androidx.appcompat.app.AppCompatActivity
import androidx.lifecycle.Observer
import androidx.lifecycle.ViewModelProvider
import cn.openp2p.OpenP2PService
import cn.openp2p.R
import cn.openp2p.databinding.ActivityLoginBinding
import openp2p.Openp2p
import kotlin.concurrent.thread
import kotlin.system.exitProcess


class LoginActivity : AppCompatActivity() {
    companion object {
        private val LOG_TAG = LoginActivity::class.simpleName
    }

    private val connection = object : ServiceConnection {
        override fun onServiceConnected(className: ComponentName, service: IBinder) {
            val binder = service as OpenP2PService.LocalBinder
            mService = binder.getService()
        }

        override fun onServiceDisconnected(className: ComponentName) {

        }
    }
    private lateinit var loginViewModel: LoginViewModel
    private lateinit var binding: ActivityLoginBinding
    private lateinit var mService: OpenP2PService
    @RequiresApi(Build.VERSION_CODES.O)
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        binding = ActivityLoginBinding.inflate(layoutInflater)
        setContentView(binding.root)

        val token = binding.token
        val login = binding.login
        val onlineState = binding.onlineState
        val openp2pLog = binding.openp2pLog
        val profile = binding.profile
        val loading = binding.loading

        loginViewModel = ViewModelProvider(this, LoginViewModelFactory())
            .get(LoginViewModel::class.java)

        loginViewModel.loginFormState.observe(this@LoginActivity, Observer {
            val loginState = it ?: return@Observer

            // disable login button unless both username / password is valid
            login.isEnabled = loginState.isDataValid

            if (loginState.passwordError != null) {
                token.error = getString(loginState.passwordError)
            }
        })
        val intent1 = VpnService.prepare(this) ?: return
        loginViewModel.loginResult.observe(this@LoginActivity, Observer {
            val loginResult = it ?: return@Observer

            loading.visibility = View.GONE
            if (loginResult.error != null) {
                showLoginFailed(loginResult.error)
            }
            if (loginResult.success != null) {
                updateUiWithUser(loginResult.success)
            }
            setResult(Activity.RESULT_OK)

            //Complete and destroy login activity once successful
            finish()
        })

        profile.setOnClickListener {
            val url = "https://console.openp2p.cn/profile"
            val i = Intent(Intent.ACTION_VIEW)
            i.data = Uri.parse(url)
            startActivity(i)
        }
        token.apply {
            afterTextChanged {
                loginViewModel.loginDataChanged(
                    "username.text.toString()",
                    token.text.toString()
                )
            }

            openp2pLog.setText(R.string.phone_setting)
            token.setText(Openp2p.getToken(getExternalFilesDir(null).toString()))
            login.setOnClickListener {
                if (login.text.toString()=="退出"){
//                    val intent = Intent(this@LoginActivity, OpenP2PService::class.java)
//                    stopService(intent)
                    Log.i(LOG_TAG, "quit")
                    mService.stop()
                    unbindService(connection)
                    val intent = Intent(this@LoginActivity, OpenP2PService::class.java)
                    stopService(intent)
                    exitAPP()

                }
                login.setText("退出")
                Log.i(LOG_TAG, "start")
                val intent = Intent(this@LoginActivity, OpenP2PService::class.java)
                intent.putExtra("token", token.text.toString())
                bindService(intent, connection, Context.BIND_AUTO_CREATE)
                startService(intent)
                thread {
                    do {
                        Thread.sleep(1000)
                        if (!::mService.isInitialized) continue
                        val isConnect = mService.isConnected()
//                        Log.i(LOG_TAG, "mService.isConnected() = " + isConnect.toString())
                        runOnUiThread {
                            if (isConnect) {
                                onlineState.setText("在线")
                            } else {
                                onlineState.setText("离线")
                            }
                        }
                    } while (true)
                }

            }
        }
    }
    @RequiresApi(Build.VERSION_CODES.LOLLIPOP)
    @SuppressLint("ServiceCast")
    fun exitAPP() {
        val activityManager =
            applicationContext?.getSystemService(Context.ACTIVITY_SERVICE) as ActivityManager
        val appTaskList = activityManager.appTasks

        for (i in appTaskList.indices) {
            appTaskList[i].finishAndRemoveTask()
        }
        exitProcess(0)
    }

    private fun updateUiWithUser(model: LoggedInUserView) {
        val welcome = getString(R.string.welcome)
        val displayName = model.displayName
        // TODO : initiate successful logged in experience
        Toast.makeText(
            applicationContext,
            "$welcome $displayName",
            Toast.LENGTH_LONG
        ).show()
    }

    private fun showLoginFailed(@StringRes errorString: Int) {
        Toast.makeText(applicationContext, errorString, Toast.LENGTH_SHORT).show()
    }
}

/**
 * Extension function to simplify setting an afterTextChanged action to EditText components.
 */
fun EditText.afterTextChanged(afterTextChanged: (String) -> Unit) {
    this.addTextChangedListener(object : TextWatcher {
        override fun afterTextChanged(editable: Editable?) {
            afterTextChanged.invoke(editable.toString())
        }

        override fun beforeTextChanged(s: CharSequence, start: Int, count: Int, after: Int) {}

        override fun onTextChanged(s: CharSequence, start: Int, before: Int, count: Int) {}
    })
}