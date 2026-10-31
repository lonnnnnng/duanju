import java.io.FileInputStream
import java.util.Properties

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

val keystoreProperties = Properties()
val keystorePropsFile = rootProject.file("keystore.properties")
val hasReleaseKeystore = keystorePropsFile.exists()
if (hasReleaseKeystore) {
    keystoreProperties.load(FileInputStream(keystorePropsFile))
}

android {
    namespace = "com.duanju.player"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.duanju.player"
        minSdk = 24
        targetSdk = 34
        versionCode = 11
        versionName = "0.1.25"
    }

    buildFeatures {
        buildConfig = true
    }

    testOptions {
        unitTests.isIncludeAndroidResources = true
    }

    // long: ffmpeg 的 .so 占了包体大头。默认不压缩存储（安装快），但直连分发时
    // 压缩能省约 10MB（23MB -> 13MB）；代价是安装时解压到应用目录。
    packaging {
        jniLibs {
            useLegacyPackaging = true
        }
    }

    signingConfigs {
        create("release") {
            if (hasReleaseKeystore) {
                storeFile = rootProject.file(keystoreProperties.getProperty("storeFile"))
                storePassword = keystoreProperties.getProperty("storePassword")
                keyAlias = keystoreProperties.getProperty("keyAlias")
                keyPassword = keystoreProperties.getProperty("keyPassword")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
            signingConfig = if (hasReleaseKeystore) signingConfigs.getByName("release") else signingConfigs.getByName("debug")
        }
    }

    compileOptions {
        // long: API 24/25 的播放密钥派生也使用 Base64，必须随包提供兼容实现。
        isCoreLibraryDesugaringEnabled = true
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }
}

dependencies {
    coreLibraryDesugaring("com.android.tools:desugar_jdk_libs:2.1.5")
    implementation("io.coil-kt:coil:2.7.0")
    implementation("androidx.core:core-ktx:1.13.1")
    implementation("androidx.appcompat:appcompat:1.7.0")
    implementation("com.google.android.material:material:1.12.0")
    implementation("androidx.recyclerview:recyclerview:1.3.2")
    implementation("androidx.swiperefreshlayout:swiperefreshlayout:1.1.0")
    implementation("androidx.media3:media3-exoplayer:1.4.1")
    implementation("androidx.media3:media3-ui:1.4.1")
    testImplementation("junit:junit:4.13.2")
    testImplementation("org.json:json:20240303")
    testImplementation("org.robolectric:robolectric:4.14.1")
    implementation("com.arthenica:ffmpeg-kit-next:9.0.2")
    implementation("com.google.android.flexbox:flexbox:3.0.0")
}
