package demo;

public aspect Logging {
    pointcut methodCall(): execution(* *(..));

    before(): methodCall() {
        System.out.println("called");
    }
}

public class Main {
    public static void main(String[] args) {
        System.out.println("hi");
    }
}
