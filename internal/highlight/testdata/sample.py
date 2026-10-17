#!/usr/bin/env python3
"""Module docstring."""
import os
from typing import List, Optional


class Demo:
    """A demo class."""

    def __init__(self, name: str, count: int = 42) -> None:
        self.name = name
        self.count = count

    def greet(self) -> str:
        # a comment
        ratio = 1.5e-3
        raw = r"raw\nstring"
        fstring = f"name={self.name} count={self.count}"
        items: List[str] = ["a", "b"]
        n = 0x1f + 0b1010 + 0o17
        if self.count > 0 and not items:
            return fstring
        return "hello, %s" % self.name


>>> x = 1
... y = 2

# type: int
z = 3
