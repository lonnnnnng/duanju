pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}
dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
        maven { url = uri(rootProject.projectDir.resolve("libs/ffmpeg-kit-next-maven").toURI()) }
    }
}
rootProject.name = "duanju-player"
include(":app")