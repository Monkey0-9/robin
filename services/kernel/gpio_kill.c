// Kernel module for GPIO-based hardware kill switch
// Uses threaded IRQ to avoid sleeping in interrupt context
// On trigger: immediately blocks all outgoing network traffic via netfilter

#include <linux/module.h>
#include <linux/kernel.h>
#include <linux/init.h>
#include <linux/gpio.h>
#include <linux/interrupt.h>
#include <linux/delay.h>
#include <linux/netfilter.h>
#include <linux/netfilter_ipv4.h>
#include <linux/atomic.h>
#include <linux/sched.h>
#include <linux/kthread.h>

MODULE_LICENSE("GPL");
MODULE_AUTHOR("Robin Trading Systems");
MODULE_DESCRIPTION("GPIO Hardware Kill Switch for Emergency Trading Halt");
MODULE_VERSION("1.0.0");

static int gpio_pin = 18;
module_param(gpio_pin, int, 0644);

static int debounce_ms = 50;
module_param(debounce_ms, int, 0644);

static atomic_t kill_switch_active = ATOMIC_INIT(0);
static unsigned int irq_number;
static struct task_struct *monitor_thread;
static struct nf_hook_ops *nf_ops = NULL;

static unsigned int kill_switch_nf_hook(void *priv, struct sk_buff *skb,
                                        const struct nf_hook_state *state) {
    if (atomic_read(&kill_switch_active)) return NF_DROP;
    return NF_ACCEPT;
}

static int register_netfilter_hook(void) {
    nf_ops = kmalloc(sizeof(struct nf_hook_ops), GFP_KERNEL);
    if (!nf_ops) return -ENOMEM;
    nf_ops->hook = kill_switch_nf_hook;
    nf_ops->pf = NFPROTO_IPV4;
    nf_ops->hooknum = NF_INET_POST_ROUTING;
    nf_ops->priority = NF_IP_PRI_FIRST;
    int ret = nf_register_net_hook(&init_net, nf_ops);
    if (ret) { kfree(nf_ops); nf_ops = NULL; }
    return ret;
}

static void unregister_netfilter_hook(void) {
    if (nf_ops) { nf_unregister_net_hook(&init_net, nf_ops); kfree(nf_ops); nf_ops = NULL; }
}

static void kill_switch_activate(void) {
    atomic_set(&kill_switch_active, 1);
    pr_alert("[KILL_SWITCH] EMERGENCY HALT on GPIO %d\n", gpio_pin);
}

// Threaded IRQ handler - runs in process context, safe to sleep
static irqreturn_t gpio_irq_handler_thread(int irq, void *dev_id) {
    msleep(debounce_ms);
    if (gpio_get_value(gpio_pin) == 1) {
        kill_switch_activate();
        return IRQ_HANDLED;
    }
    return IRQ_NONE;
}

static int monitor_kill_switch(void *data) {
    while (!kthread_should_stop()) {
        if (atomic_read(&kill_switch_active)) {
            schedule_timeout_interruptible(HZ);
            continue;
        }
        if (gpio_get_value(gpio_pin) == 1) kill_switch_activate();
        schedule_timeout_interruptible(HZ / 100);
    }
    return 0;
}

static int __init kill_switch_init(void) {
    int ret;
    if (!gpio_is_valid(gpio_pin)) return -EINVAL;
    ret = gpio_request_one(gpio_pin, GPIOF_IN, "kill_switch");
    if (ret) return ret;
    irq_number = gpio_to_irq(gpio_pin);
    if (irq_number < 0) { gpio_free(gpio_pin); return irq_number; }
    ret = request_threaded_irq(irq_number, NULL, gpio_irq_handler_thread,
                                IRQF_TRIGGER_RISING | IRQF_ONESHOT, "kill_switch", NULL);
    if (ret) { gpio_free(gpio_pin); return ret; }
    ret = register_netfilter_hook();
    if (ret) { free_irq(irq_number, NULL); gpio_free(gpio_pin); return ret; }
    monitor_thread = kthread_run(monitor_kill_switch, NULL, "kill_switch_monitor");
    if (IS_ERR(monitor_thread)) {
        unregister_netfilter_hook(); free_irq(irq_number, NULL);
        gpio_free(gpio_pin); return PTR_ERR(monitor_thread);
    }
    pr_info("[KILL_SWITCH] Module loaded. GPIO %d armed.\n", gpio_pin);
    return 0;
}

static void __exit kill_switch_exit(void) {
    if (monitor_thread) kthread_stop(monitor_thread);
    unregister_netfilter_hook();
    if (irq_number) free_irq(irq_number, NULL);
    gpio_free(gpio_pin);
    pr_info("[KILL_SWITCH] Module unloaded\n");
}

module_init(kill_switch_init);
module_exit(kill_switch_exit);
