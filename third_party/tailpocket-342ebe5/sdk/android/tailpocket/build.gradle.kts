plugins {
    `maven-publish`
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.serialization)
}

android {
    namespace = "io.jingtao.tailpocket"
    compileSdk = 36
    defaultConfig {
        minSdk = 26
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
        consumerProguardFiles("consumer-rules.pro")
    }
    publishing { singleVariant("release") }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

dependencies {
    api(libs.tailpocket.mobile)
    implementation(libs.kotlinx.coroutines.android)
    implementation(libs.kotlinx.serialization.json)
    testImplementation(libs.junit)
    testImplementation(libs.kotlinx.coroutines.test)
    androidTestImplementation(libs.androidx.test.runner)
    androidTestImplementation("androidx.annotation:annotation:1.9.1") // newer than the runner's 1.7.0
    androidTestImplementation(libs.androidx.test.ext.junit)
    androidTestImplementation(libs.kotlinx.coroutines.test)
}

// ./gradlew publish puts io.jingtao.tailpocket:tailpocket:0.3.0-pre.6 next to the gomobile library in
// build/android/m2 (React Native's module and any app can depend on it from there).
publishing {
    publications {
        register<MavenPublication>("release") {
            groupId = "io.jingtao.tailpocket"
            artifactId = "tailpocket"
            version = "0.3.0-pre.6"
            afterEvaluate { from(components["release"]) }
        }
    }
    repositories { maven { url = uri(rootDir.resolve("../../build/android/m2")) } }
}
