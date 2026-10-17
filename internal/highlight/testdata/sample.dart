import 'dart:async';

/// A demo class with **markdown** docs.
/// See [the docs](https://dart.dev).
class Demo extends Base {
  final String name;
  final int count = 42;
  const Demo(this.name);

  Future<void> run(List<String> items) async {
    final ratio = 1.5e-3;
    final raw = r'raw \n string';
    final multi = '''multi
line''';
    final label = 'demo $name';
    final map = <String, int>{'a': 1};
    if (count > 0 && items.isNotEmpty) {
      print('$label $count $ratio $raw $multi $map');
    }
  }
}
