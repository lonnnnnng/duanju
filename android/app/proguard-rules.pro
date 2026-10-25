# 依赖库（Media3 / AppCompat / Material / RecyclerView / SwipeRefreshLayout）自带的
# consumer 规则会自动应用，这里只做少量补充。

# 保留注解信息（如 @UnstableApi 等，便于问题排查）。
-keepattributes *Annotation*, InnerClasses, Signature

# 播放器组件可能通过反射/资源名访问，保留 media3 的控制器与 UI。
-keep class androidx.media3.** { *; }
-keep class com.google.android.exoplayer2.** { *; }

# FFmpegKitNext：协议白名单等字符串常量与 native 桥接类不能被裁剪/改名
-keep class com.arthenica.ffmpegkit.** { *; }
-dontwarn com.arthenica.ffmpegkit.**