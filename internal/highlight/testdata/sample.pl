#!/usr/bin/env perl
use strict;
use warnings;
use feature 'say';

# a comment
my $name = "pier";
my @items = (1, 2, 3);
my %map = (name => 'pier', count => 42);
my $ref = { key => 'value' };

=pod
A block comment.
=cut

sub describe {
    my ($label, $count) = @_;
    my $interp = "name=$name count=$count";
    my $raw = 'single quoted';
    my $qq = qq{interpolated $name};
    my $re = qr/[a-z]+\/[0-9]{2}/i;
    my $match = $label =~ m/(\w+)/;
    (my $sub = $label) =~ s/\s+/_/g;
    (my $tr = $label) =~ tr/a-z/A-Z/;
    my $num = 0x1f + 0b1010 + 1.5e-3;
    say $interp, $raw, $qq, $re, $match, $sub, $tr, $num;
    return $label;
}

describe($name, 42);
