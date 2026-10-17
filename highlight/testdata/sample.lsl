// a comment
/* block comment */
default
{
    state_entry()
    {
        integer count = 3;
        float x = 1.5;
        string name = "world";
        vector pos = <1.0, 2.0, 3.0>;
        rotation rot = ZERO_ROTATION;
        llSay(0, "Hello, " + name);
        if (count > 0)
        {
            llOwnerSay("count is " + (string)count);
        }
    }

    touch_start(integer total)
    {
        llResetScript();
    }
}
