plugins {
    `maven-publish`
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.compose)
}

android {
    namespace = "io.jingtao.tailpocket.ui"
    compileSdk = 36
    defaultConfig { minSdk = 26 }
    publishing { singleVariant("release") }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    buildFeatures { compose = true }
}

dependencies {
    api(project(":tailpocket"))
    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.foundation)
    implementation(libs.androidx.compose.material3)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.core.ktx)
    implementation(libs.kotlinx.coroutines.android)
    // Lifts the version activity-compose asks for to the one the core library already uses.
    implementation(libs.kotlinx.serialization.json)
    implementation(libs.androidx.camera.core)
    implementation(libs.androidx.camera.camera2)
    implementation(libs.androidx.camera.lifecycle)
    implementation(libs.androidx.camera.view)
    implementation(libs.zxing.core)
}

// ./gradlew publish puts io.jingtao.tailpocket:tailpocket-ui:0.3.6 next to the gomobile library in
// build/android/m2 (React Native's module and any app can depend on it from there).
publishing {
    publications {
        register<MavenPublication>("release") {
            groupId = "io.jingtao.tailpocket"
            artifactId = "tailpocket-ui"
            version = "0.3.6"
            afterEvaluate { from(components["release"]) }
        }
    }
    repositories { maven { url = uri(rootDir.resolve("../../build/android/m2")) } }
}
