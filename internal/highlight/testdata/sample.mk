# Makefile sample
CC := gcc
CFLAGS = -Wall -O2
SRC = $(wildcard src/*.c)
OBJ = $(patsubst src/%.c,obj/%.o,$(SRC))

all: $(OBJ)
	$(CC) $(CFLAGS) -o app $(OBJ)

obj/%.o: src/%.c
	@mkdir -p $(@D)
	$(CC) $(CFLAGS) -c $< -o $@

.PHONY: all clean
clean:
	rm -f app $(OBJ)

include config.mk
