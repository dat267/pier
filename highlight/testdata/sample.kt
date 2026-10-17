package com.example

import kotlin.collections.List

/**
 * A demo class.
 * @author pier
 */
class Demo(val name: String, private var count: Int = 42) : Runnable {
    override fun run() {
        val ratio = 1.5e-3
        val raw = """
            raw
            string
        """
        val items: List<String> = listOf("a", "b")
        val n = 0x1f + 0b1010 + 0o17
        if (count > 0 && items.isNotEmpty()) {
            println("hello, ${name} $count $ratio $n")
        }
        return this@Demo
    }
}

interface Greet { fun greet(): Unit }
