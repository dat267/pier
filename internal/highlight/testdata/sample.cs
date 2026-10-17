using System;
using System.Collections.Generic;

namespace Demo
{
    /// <summary>A demo class.</summary>
    /// <remarks>More.</remarks>
    public class Box<T> where T : class
    {
        private readonly T value;
        public const int Max = 42;

        public Box(T value) => this.value = value;

        public string Describe(int count = 0)
        {
            var verbatim = @"raw ""string"" here";
            var interpolated = $"value={value} count={count}";
            var numbers = 0x1f + 0b1010 + 1.5e-3;
            var list = new List<string> { "a", "b" };
            if (count > 0 && list.Count != 0)
            {
                return $"{verbatim} {interpolated} {numbers}";
            }
            return "hello, " + value;
        }
    }

    interface IGreet { void Greet(); }
}
