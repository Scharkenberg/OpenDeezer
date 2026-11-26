// Top-level build file. Plugin versions are declared here and applied per-module.
// AGP compiles Kotlin itself (built-in Kotlin); the classpath entry sets its Kotlin version.
buildscript {
    dependencies {
        classpath("org.jetbrains.kotlin:kotlin-gradle-plugin:2.4.21")
    }
}

plugins {
    id("com.android.application") version "9.5.2" apply false
    id("org.jetbrains.kotlin.plugin.compose") version "2.4.21" apply false
}
