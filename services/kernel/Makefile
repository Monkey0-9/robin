# Linux kernel module build for GPIO kill switch
# Standard KBuild infrastructure
# Usage: make -C /lib/modules/$(uname -r)/build M=$(PWD) modules

obj-m += gpio_kill.o
gpio_kill-y := gpio_kill.o

all:
	make -C /lib/modules/$(shell uname -r)/build M=$(PWD) modules

clean:
	make -C /lib/modules/$(shell uname -r)/build M=$(PWD) clean
	rm -f *.o *.ko *.mod* Module* modules*
