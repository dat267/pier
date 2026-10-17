#!/usr/bin/env groovy
package com.example

/** A demo. @author pier */
class Demo implements Runnable {
    static final String NAME = "pier"
    static final int COUNT = 42
    static final double RATIO = 1.5e-3
    static final String RAW = '''raw
string'''
    static final String SLASHY = $/slashy $NAME string/$

    @Override
    void run() {
        def map = [name: NAME, count: COUNT]
        def ratio = ~/[0-9]+\.[0-9]+/
        def items = ['a', 'b']
        if (COUNT > 0 && !items.isEmpty()) {
            println("hello, ${NAME} ${COUNT}")
        }
        assert map.size() == 2
    }
}

trait Greet { void greet() }
