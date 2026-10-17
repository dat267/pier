//! Crate docs
use std::collections::HashMap;

/// A demo struct.
#[derive(Debug, Clone)]
pub struct Demo<'a> {
    name: &'a str,
    count: i32,
    ratio: f64,
}

impl<'a> Demo<'a> {
    pub fn new(name: &'a str) -> Self {
        let s = "plain";
        let raw = r#"raw "string""#;
        let byte = b'x';
        let n: u32 = 0x1f_u32;
        let f = 1.5e-3f64;
        if n > 0 && !s.is_empty() {
            println!("{} {} {}", name, n, f);
        }
        Self { name, count: n as i32, ratio: f }
    }
}

trait Greet { fn greet(&self); }
