package com.example

import scala.collection.mutable

/** A demo. */
object Demo extends App {
  val name: String = "pier"
  val count: Int = 42
  val ratio: Double = 1.5e-3
  val sym: Symbol = 'hello
  val raw = s"name=$name count=$count"
  val multi = """line1
line2"""

  def greet(who: String): Unit = {
    println(s"hello, $who")
  }

  case class Point(x: Int, y: Int)

  trait Greet { def greet(): Unit }

  if (count > 0 && name.nonEmpty) greet(name)
}
