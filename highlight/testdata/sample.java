package com.example;

import java.util.List;

/**
 * A demo class.
 * @author pier
 */
public class Demo implements Runnable {
	private static final String NAME = "pier";
	private final int count = 42;
	private final double ratio = 1.5e-3;
	private final char c = 'x';

	@Override
	public void run() {
		List<String> items = List.of(NAME, "a", "b");
		for (String item : items) {
			System.out.println(item + count + ratio + c);
		}
	}

	record Point(int x, int y) {}
}
