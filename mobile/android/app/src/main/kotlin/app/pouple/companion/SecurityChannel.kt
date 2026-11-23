package app.pouple.companion

import android.os.Build
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyPermanentlyInvalidatedException
import android.security.keystore.KeyProperties
import android.security.keystore.StrongBoxUnavailableException
import android.util.Base64
import androidx.biometric.BiometricManager
import androidx.biometric.BiometricManager.Authenticators.BIOMETRIC_STRONG
import androidx.biometric.BiometricManager.Authenticators.BIOMETRIC_WEAK
import androidx.biometric.BiometricManager.Authenticators.DEVICE_CREDENTIAL
import androidx.biometric.BiometricPrompt
import androidx.core.content.ContextCompat
import androidx.fragment.app.FragmentActivity
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.PrivateKey
import java.security.Signature
import java.security.spec.ECGenParameterSpec

/**
 * The phone's step-up key and its biometric prompts.
 *
 * The key is an EC P-256 key in the Android Keystore that can only sign after the person authenticates with a strong
 * biometric for each signature (setUserAuthenticationRequired, per-use). The Mac checks signatures with the public key
 * sent at pairing. Errors reach Dart as codes: cancelled, unavailable, invalidated, failed. No message or key material
 * is ever logged.
 */
class SecurityChannel(private val activity: FragmentActivity) : MethodChannel.MethodCallHandler {
    companion object {
        const val NAME = "app.pouple.companion/security"
        private const val ALIAS = "pouple_stepup_v1"
        private const val PROVIDER = "AndroidKeyStore"
    }

    /** Replies at most once: BiometricPrompt callbacks can overlap (an error after a success, for example). */
    private class Reply(private val result: MethodChannel.Result) {
        private var done = false

        fun success(value: Any?) {
            if (done) return
            done = true
            result.success(value)
        }

        fun error(code: String) {
            if (done) return
            done = true
            result.error(code, null, null)
        }
    }

    override fun onMethodCall(call: MethodCall, result: MethodChannel.Result) {
        val reply = Reply(result)
        try {
            when (call.method) {
                "createKey" -> createKey(reply)
                "sign" -> sign(
                    call.argument<String>("message") ?: return reply.error("failed"),
                    call.argument<String>("title") ?: "Confirm",
                    call.argument<String>("subtitle") ?: "",
                    reply,
                )
                "deleteKey" -> {
                    keyStore().deleteEntry(ALIAS)
                    reply.success(true)
                }
                "authenticate" -> authenticate(call.argument<String>("title") ?: "Unlock", reply)
                "deviceName" -> reply.success(deviceName())
                else -> result.notImplemented()
            }
        } catch (_: Exception) {
            reply.error("failed")
        }
    }

    private fun keyStore(): KeyStore = KeyStore.getInstance(PROVIDER).apply { load(null) }

    private fun deviceName(): String {
        val model = Build.MODEL ?: ""
        val maker = Build.MANUFACTURER ?: ""
        val name = if (model.startsWith(maker, ignoreCase = true)) model else "$maker $model"
        return name.trim().replaceFirstChar { it.uppercase() }.ifEmpty { "Android phone" }
    }

    private fun createKey(reply: Reply) {
        val manager = BiometricManager.from(activity)
        if (manager.canAuthenticate(BIOMETRIC_STRONG) != BiometricManager.BIOMETRIC_SUCCESS) {
            reply.error("unavailable")
            return
        }
        keyStore().deleteEntry(ALIAS)
        val generator = KeyPairGenerator.getInstance(KeyProperties.KEY_ALGORITHM_EC, PROVIDER)
        val pair = try {
            generator.initialize(keySpec(strongBox = true))
            generator.generateKeyPair()
        } catch (_: StrongBoxUnavailableException) {
            generator.initialize(keySpec(strongBox = false))
            generator.generateKeyPair()
        }
        // PublicKey.getEncoded() is the X.509 SubjectPublicKeyInfo DER the Mac expects, in padded standard base64.
        reply.success(Base64.encodeToString(pair.public.encoded, Base64.NO_WRAP))
    }

    private fun keySpec(strongBox: Boolean): KeyGenParameterSpec {
        val builder = KeyGenParameterSpec.Builder(ALIAS, KeyProperties.PURPOSE_SIGN)
            .setAlgorithmParameterSpec(ECGenParameterSpec("secp256r1"))
            .setDigests(KeyProperties.DIGEST_SHA256)
            .setUserAuthenticationRequired(true)
            .setInvalidatedByBiometricEnrollment(true)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            // Timeout 0: the person authenticates for every signature, with a strong biometric.
            builder.setUserAuthenticationParameters(0, KeyProperties.AUTH_BIOMETRIC_STRONG)
        }
        if (strongBox && Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            builder.setIsStrongBoxBacked(true)
        }
        return builder.build()
    }

    private fun sign(message: String, title: String, subtitle: String, reply: Reply) {
        val key = keyStore().getKey(ALIAS, null) as? PrivateKey
        if (key == null) {
            reply.error("invalidated")
            return
        }
        val signature = Signature.getInstance("SHA256withECDSA")
        try {
            signature.initSign(key)
        } catch (_: KeyPermanentlyInvalidatedException) {
            reply.error("invalidated")
            return
        }
        val callback = object : BiometricPrompt.AuthenticationCallback() {
            override fun onAuthenticationSucceeded(result: BiometricPrompt.AuthenticationResult) {
                try {
                    val crypto = result.cryptoObject?.signature ?: return reply.error("failed")
                    crypto.update(message.toByteArray(Charsets.UTF_8))
                    reply.success(Base64.encodeToString(crypto.sign(), Base64.NO_WRAP))
                } catch (_: Exception) {
                    reply.error("failed")
                }
            }

            override fun onAuthenticationError(errorCode: Int, errString: CharSequence) {
                reply.error(errorCodeOf(errorCode))
            }
        }
        val info = BiometricPrompt.PromptInfo.Builder()
            .setTitle(title)
            .setSubtitle(subtitle)
            .setNegativeButtonText("Cancel")
            .setAllowedAuthenticators(BIOMETRIC_STRONG)
            .setConfirmationRequired(false)
            .build()
        BiometricPrompt(activity, ContextCompat.getMainExecutor(activity), callback)
            .authenticate(info, BiometricPrompt.CryptoObject(signature))
    }

    private fun authenticate(title: String, reply: Reply) {
        val allowed = BIOMETRIC_WEAK or DEVICE_CREDENTIAL
        if (BiometricManager.from(activity).canAuthenticate(allowed) != BiometricManager.BIOMETRIC_SUCCESS) {
            reply.error("unavailable")
            return
        }
        val callback = object : BiometricPrompt.AuthenticationCallback() {
            override fun onAuthenticationSucceeded(result: BiometricPrompt.AuthenticationResult) {
                reply.success(true)
            }

            override fun onAuthenticationError(errorCode: Int, errString: CharSequence) {
                reply.error(errorCodeOf(errorCode))
            }
        }
        val info = BiometricPrompt.PromptInfo.Builder()
            .setTitle(title)
            .setAllowedAuthenticators(allowed)
            .build()
        BiometricPrompt(activity, ContextCompat.getMainExecutor(activity), callback).authenticate(info)
    }

    private fun errorCodeOf(code: Int): String = when (code) {
        BiometricPrompt.ERROR_USER_CANCELED,
        BiometricPrompt.ERROR_NEGATIVE_BUTTON,
        BiometricPrompt.ERROR_CANCELED -> "cancelled"
        BiometricPrompt.ERROR_HW_NOT_PRESENT,
        BiometricPrompt.ERROR_HW_UNAVAILABLE,
        BiometricPrompt.ERROR_NO_BIOMETRICS,
        BiometricPrompt.ERROR_NO_DEVICE_CREDENTIAL -> "unavailable"
        else -> "failed"
    }
}
