import Foundation

precedencegroup PowerPrecedence {
  higherThan: MultiplicationPrecedence
}

infix operator **: PowerPrecedence

@available(iOS 13.0, *)
public struct Box<T> {
  public var value: T
  let count: Int = 42
  var ratio: Double = 1.5e-3

  public init(value: T) {
    self.value = value
  }

  public subscript(index: Int) -> T? {
    return index == 0 ? value : nil
  }

  public func describe(name label: String, _ extra: Int = 0) -> String {
    let raw = #"raw \n string"#
    let multi = """
      line one
      line two
      """
    let label2 = "name=\(label) count=\(count)"
    let tuple = (x: 1, y: 2)
    let hex = 0x1f, bin = 0b1010
    if count > 0 && !label.isEmpty {
      print(raw, multi, label2, tuple.x, hex, bin)
    }
    return label2
  }
}

extension Box: Equatable where T: Equatable {
  static func == (lhs: Box, rhs: Box) -> Bool {
    return lhs.value == rhs.value
  }
}

func operator ** (base: Double, power: Double) -> Double {
  return pow(base, power)
}
