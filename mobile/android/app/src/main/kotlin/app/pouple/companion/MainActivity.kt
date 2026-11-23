package app.pouple.companion

import android.os.Bundle
import android.view.WindowManager
import io.flutter.embedding.android.FlutterFragmentActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel

/**
 * The single activity. It is a fragment activity because BiometricPrompt needs one, and it blocks screenshots and
 * the recent-apps preview (FLAG_SECURE): the screen shows private conversations.
 */
class MainActivity : FlutterFragmentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        window.setFlags(WindowManager.LayoutParams.FLAG_SECURE, WindowManager.LayoutParams.FLAG_SECURE)
        super.onCreate(savedInstanceState)
    }

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        val security = SecurityChannel(this)
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, SecurityChannel.NAME)
            .setMethodCallHandler(security)
    }
}
