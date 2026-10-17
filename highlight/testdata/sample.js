#!/usr/bin/env node
'use strict';

/**
 * A demo class.
 * @param {string} name the name
 * @returns {Demo}
 */
class Demo extends Base {
  #count = 42;
  constructor(name) {
    super();
    this.name = name;
  }

  get label() { return `demo ${this.name}`; }
  set label(value) { this.name = value; }

  async run(items) {
    // a line comment
    /* a block comment */
    const ratio = 1.5e-3;
    const hex = 0x1f, bin = 0b1010, big = 123n;
    const re = /[a-z]+\/[0-9]{2}/gi;
    const arrow = (a, b) => a + b;
    const obj = { name: 'pier', count: hex, nested: { ok: true } };
    const [first, ...rest] = items;
    const html = html`<div class="box">${this.name}</div>`;
    const css = css`.box { color: #fff; }`;
    if (this.#count > 0 && rest.length) {
      console.log(html, css, ratio, bin, big, re, arrow(1, 2), obj, first);
    }
    return <Box name={this.name} count={this.#count} />;
  }
}

function helper(a, b) {
  return a * b;
}

const fn = function named(x) { return x; };
export default Demo;
