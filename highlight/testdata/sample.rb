#!/usr/bin/env ruby
# a comment
=begin
block comment
=end

module Demo
  MAX = 42
  class Box
    attr_reader :value

    def initialize(value:, count: 0)
      @value = value
      @count = count
      @@all ||= []
    end

    def describe(label = "x")
      raw = 'single quoted'
      interp = "name=#{@value} count=#{@count}"
      heredoc = <<~EOT
        heredoc #{@value} text
      EOT
      regex = /[a-z]+\/[0-9]{2}/
      numbers = 0x1f + 0b1010 + 0o17 + 1.5e-3
      sym = :value
      puts raw, interp, heredoc, regex, numbers, sym, label
    end
  end
end

>> x = 1
Demo::Box.new(value: 1).describe
