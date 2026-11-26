import com.android.build.api.variant.FilterConfiguration

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.plugin.compose")
}

val upstreamUpdatesEnabled = providers.gradleProperty("upstreamUpdates")
    .orNull
    ?.toBooleanStrictOrNull()
    ?: true

// Keep this order in sync with F-Droid's VercodeOperation entries. Compatible
// devices can accept more than one ABI, so the preferred ABI needs the larger
// version code. The base versionCode remains the release counter that tooling
// reads and bumps; split APKs append the stable ABI digit below.
val abiVersionCodeOffsets = linkedMapOf(
    "armeabi-v7a" to 1,
    "arm64-v8a" to 2,
    "x86" to 3,
    "x86_64" to 4,
)
val fdroidAbi = providers.gradleProperty("fdroidAbi").orNull
if (fdroidAbi != null && fdroidAbi !in abiVersionCodeOffsets) {
    throw GradleException(
        "Unsupported fdroidAbi '$fdroidAbi'; expected one of ${abiVersionCodeOffsets.keys.joinToString()}",
    )
}
if (fdroidAbi != null && upstreamUpdatesEnabled) {
    throw GradleException("fdroidAbi requires -PupstreamUpdates=false")
}
val abiSplitsEnabled =
    providers.gradleProperty("abiSplits").orNull == "true" || fdroidAbi != null

android {
    namespace = "dev.scharkenberg.purpledisc"
    compileSdk {
        version = release(37) {
            minorApiLevel = 2
        }
    }
    buildToolsVersion = "37.0.0"

    defaultConfig {
        applicationId = "dev.scharkenberg.purpledisc"
        minSdk = 24
        targetSdk = 37
        versionCode = 27
        versionName = "4.0.0"
        buildConfigField("boolean", "ENABLE_UPSTREAM_UPDATES", upstreamUpdatesEnabled.toString())
        // -Pabi=<ABI> (release.ps1): an APK with the engine for that one CPU type.
        providers.gradleProperty("abi").orNull?.let { ndk { abiFilters += it } }
    }

    // The app's seven languages; the libraries' other translations stay out.
    androidResources {
        localeFilters += listOf("en", "ar", "es", "fr", "hi", "ru", "zh-rCN")
    }

    // Release signing is driven by env vars set by CI from repo secrets (see
    // .github/workflows/release.yml + docs/ANDROID_SIGNING.md). The keystore is
    // PKCS12; its single password is used for both store and key. CI verifies all
    // inputs before Gradle starts. When absent locally, a plain release build is
    // unsigned so developers can still compile without access to the project key.
    signingConfigs {
        create("release") {
            val ksPath = System.getenv("ANDROID_KEYSTORE_FILE")
            if (ksPath != null && file(ksPath).exists()) {
                storeFile = file(ksPath)
                storeType = "PKCS12"
                storePassword = System.getenv("ANDROID_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("ANDROID_KEY_ALIAS")
                keyPassword = System.getenv("ANDROID_KEY_PASSWORD")
                // Google's sideload developer verification checks the APK Signature
                // Scheme v2/v3 certificate against the registered developer key, so
                // v2/v3 must be present. No v1: Android 7 (minSdk 24) reads v2.
                enableV1Signing = false
                enableV2Signing = true
                enableV3Signing = true
            }
        }
    }

    buildTypes {
        release {
            // R8 code and resource optimization. The engine's JNI classes are kept by the rules the
            // gomobile AAR carries (proguard.txt: go.**, pdmobile.**).
            optimization {
                enable = true
                keepRules { files.add(file("proguard-rules.pro")) }
            }
            // Sign with the release key only when the keystore env is present (CI
            // with secrets); otherwise leave unsigned so local builds still work.
            if (System.getenv("ANDROID_KEYSTORE_FILE") != null) {
                signingConfig = signingConfigs.getByName("release")
            }
        }
    }

    // Store/release builds can emit one APK per ABI. Normal releases retain a
    // universal APK; an F-Droid build selects exactly one ABI so fdroidserver
    // sees a single output without a custom `output` metadata field.
    splits {
        abi {
            isEnable = abiSplitsEnabled
            reset()
            if (fdroidAbi == null) {
                include(*abiVersionCodeOffsets.keys.toTypedArray())
            } else {
                include(fdroidAbi)
            }
            isUniversalApk = fdroidAbi == null
        }
    }

    // Two form factors from one engine: the touch phone/tablet app and a
    // D-pad-driven Android TV app. Shared code lives in src/main; each flavor
    // supplies its own launcher activity + manifest (src/mobile, src/tv).
    flavorDimensions += "device"
    productFlavors {
        create("mobile") {
            dimension = "device"
        }
        create("tv") {
            dimension = "device"
            applicationIdSuffix = ".tv"
            versionNameSuffix = "-tv"
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_21
        targetCompatibility = JavaVersion.VERSION_21
    }

    buildFeatures {
        buildConfig = true
        compose = true
    }

    packaging {
        resources {
            excludes += "/META-INF/{AL2.0,LGPL2.1}"
            excludes += listOf("/META-INF/**/LICENSE.txt", "/kotlin/**.kotlin_builtins", "DebugProbesKt.bin")
            // The libraries' version stamps, read only by tools that inspect an APK.
            excludes += "/META-INF/*.version"
        }
    }
}

if (abiSplitsEnabled) {
    val baseVersionCode = android.defaultConfig.versionCode
        ?: throw GradleException("Android versionCode must be configured")
    if (baseVersionCode !in 1..209_999_999) {
        throw GradleException("Android base versionCode is outside the ABI-safe range")
    }

    androidComponents.onVariants(
        androidComponents.selector().withBuildType("release"),
    ) { variant ->
        variant.outputs.forEach { output ->
            val abi = output.filters
                .firstOrNull { it.filterType == FilterConfiguration.FilterType.ABI }
                ?.identifier
            val offset = if (abi == null) 0 else abiVersionCodeOffsets.getValue(abi)
            output.versionCode.set(baseVersionCode * 10 + offset)
        }
    }
}

dependencies {
    // Purple Disc Go engine, bound by gomobile (built by CI into libs/pdmobile.aar).
    implementation(files("libs/pdmobile.aar"))

    val composeBom = platform("androidx.compose:compose-bom:2026.11.01")
    implementation(composeBom)

    implementation("androidx.core:core-ktx:1.19.1")
    // The media session, its notification and the media button receiver (Media3).
    implementation("androidx.media3:media3-session:1.11.1")
    implementation("androidx.activity:activity-compose:1.13.0")
    implementation("androidx.lifecycle:lifecycle-runtime-ktx:2.11.1")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.11.1")
    implementation("androidx.lifecycle:lifecycle-process:2.11.1")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.11.1")

    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-graphics")
    implementation("androidx.compose.foundation:foundation")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.material:material-icons-extended")

    implementation("androidx.navigation:navigation-compose:2.10.2")

    // Foldable posture (FoldingFeature: tabletop/book) for the split layouts.
    implementation("androidx.window:window:1.5.1")

    implementation("io.coil-kt.coil3:coil-compose:3.6.3")
    // Coil's network fetcher without a client of its own: covers download through the engine.
    implementation("io.coil-kt.coil3:coil-network-core:3.6.3")

}
// Inter is kept once, in the repository's assets/fonts: res/font takes it under a lower-case
// name, and its license goes with the app's assets. Both copies are git-ignored.
val interFont = tasks.register<Copy>("interFont") {
    from(rootProject.file("../../assets/fonts/InterVariable.ttf")) { rename { "inter.ttf" } }
    // Static weights for Android 7, which cannot open the variable file.
    from(rootProject.file("../../assets/fonts/Inter-Regular.ttf")) { rename { "inter_regular.ttf" } }
    from(rootProject.file("../../assets/fonts/Inter-SemiBold.ttf")) { rename { "inter_semibold.ttf" } }
    into(layout.projectDirectory.dir("src/main/res/font"))
}
val interLicense = tasks.register<Copy>("interLicense") {
    from(rootProject.file("../../assets/fonts/Inter-OFL.txt"))
    into(layout.projectDirectory.dir("src/main/assets/fonts"))
}
tasks.named("preBuild") { dependsOn(interFont, interLicense) }
