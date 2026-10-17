<?php
namespace Demo;

use App\Box;

/**
 * A demo class.
 * @author pier
 */
class Demo implements Runnable
{
    private const MAX = 42;

    public function run(string $name, int $count = 0): void
    {
        $verbatim = "raw $name string";
        $single = 'single quoted';
        $heredoc = <<<EOT
        heredoc $name text
        EOT;
        $numbers = 0x1f + 0b1010 + 0o17 + 1.5e-3;
        $items = array('a', 'b');
        if ($count > 0 && !empty($items)) {
            echo "hello, {$name} $count";
        }
    }
}
?>
