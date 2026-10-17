.class public Lcom/example/Main;
.super Ljava/lang/Object;
.source "Main.java"

# a comment

.method public static main([Ljava/lang/String;)V
    .registers 2
    const-string v0, "Hello"
    invoke-static {v0}, Lfoo/Bar;->baz(Ljava/lang/String;)V
    return-void
.end method
