'use strict';

/** A demo class. */
@sealed
export default class Demo<T extends object> implements Runnable {
  private readonly count: number = 42;
  protected items: string[] = [];

  constructor(public name: string, private config: Config = {}) {
    super();
  }

  async run(items?: T[]): Promise<void> {
    const ratio: number = 1.5e-3;
    const re: RegExp = /[a-z]+/gi;
    const label: string = `demo ${this.name}`;
    const obj: Config = { name: 'pier', count: this.count };
    if (this.count > 0 && items?.length) {
      console.log(label, obj, ratio, re);
    }
    return <div className="box">{this.name}</div>;
  }
}

namespace Demo.Utils {
  export function helper(a: number, b: number): number {
    return a * b;
  }
}

interface Config {
  name?: string;
  count?: number;
}

type Alias = Config | null;

enum Color { Red, Green, Blue }

const fn: (x: number) => number = (x) => x;
