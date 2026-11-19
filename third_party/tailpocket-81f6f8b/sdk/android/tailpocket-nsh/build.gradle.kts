plugins {
    alias(libs.plugins.android.library)
}

// NSH's runtime (nsh.aar) ships with the app that embeds NSH, so this module only compiles against it.
val nshAar = providers.environmentVariable("NSH_AAR").orElse("${System.getProperty("user.home")}/.nsh/embed/android/nsh.aar").get()

android {
    namespace = "io.jingtao.tailpocket.nsh"
    compileSdk = 36
    defaultConfig {
        minSdk = 28
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }
    // The example NSH app at assets/nsh-phone, built by scripts/e2e-nsh-android.sh.
    sourceSets["androidTest"].assets.srcDir(rootDir.resolve("../../build/nsh-android-assets"))
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

dependencies {
    api(project(":tailpocket"))
    implementation(project(":tailpocket-ui"))
    implementation(libs.kotlinx.coroutines.android)
    implementation(libs.kotlinx.serialization.json)
    compileOnly(files(zipTree(nshAar).matching { include("classes.jar") }))
    androidTestImplementation(files(nshAar))
    androidTestImplementation(libs.androidx.test.runner)
    androidTestImplementation("androidx.annotation:annotation:1.9.1") // newer than the runner's 1.7.0
    androidTestImplementation(libs.androidx.test.ext.junit)
}
