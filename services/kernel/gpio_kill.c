// Kernel module for GPIO-based hardware kill switch
// Uses descriptor-based GPIO API (modern replacement for legacy gpio_* API)
// Threaded IRQ to avoid sleeping in interrupt context
//
// SAFETY FIX v2.1.0: Kill switch now blocks only TRADING traffic on
// configurable port ranges. Management traffic (SSH, health checks, admin)
// is explicitly whitelisted and continues to flow.
// Previously this blocked ALL IPv4 traffic which was dangerous.

#include <linux/module.h>
#include <linux/kernel.h>
#include <linux/init.h>
#include <linux/gpio/consumer.h>
#include <linux/interrupt.h>
#include <linux/delay.h>
#include <linux/netfilter.h>
#include <linux/netfilter_ipv4.h>
#include <linux/atomic.h>
#include <linux/sched.h>
#include <linux/kthread.h>
#include <linux/tcp.h>
#include <linux/udp.h>
#include <linux/ip.h>
#include <linux/skbuff.h>

MODULE_LICENSE("GPL");
MODULE_AUTHOR("Robin Trading Systems");
MODULE_DESCRIPTION("GPIO Kill Switch v2.1.0 - Emergency Trading Halt (whitelist-aware)");
MODULE_VERSION("2.1.0");

/* Configurable parameters */
static int gpio_pin = 18;
module_param(gpio_pin, int, 0644);
MODULE_PARM_DESC(gpio_pin, "GPIO pin number for kill switch input");

static int debounce_ms = 50;
module_param(debounce_ms, int, 0644);
MODULE_PARM_DESC(debounce_ms, "Debounce delay in milliseconds");

/* Management port whitelist — traffic on these ports is NEVER blocked.
 * Add SSH (22), health-check (8080), admin (9090) etc. here.
 * Trading traffic ports (5000-5100, 9091-9095) will be blocked.
 */
static int whitelist_ports[16] = {22, 8080, 8443, 9090, 2222, 0};
static int whitelist_ports_count = 5;
module_param_array(whitelist_ports, int, &whitelist_ports_count, 0644);
MODULE_PARM_DESC(whitelist_ports, "Comma-separated management ports to whitelist from kill switch");

/* Trading port range to block when kill switch activates */
static int trading_port_min = 5000;
module_param(trading_port_min, int, 0644);
MODULE_PARM_DESC(trading_port_min, "Minimum trading port (inclusive) to block on kill");

static int trading_port_max = 9100;
module_param(trading_port_max, int, 0644);
MODULE_PARM_DESC(trading_port_max, "Maximum trading port (inclusive) to block on kill");

static struct gpio_desc *kill_gpio = NULL;
static struct gpio_desc *monitor_gpio = NULL;
static atomic_t kill_switch_active = ATOMIC_INIT(0);
static unsigned int irq_number;
static struct task_struct *monitor_thread;
static struct nf_hook_ops *nf_ops = NULL;

/* Check if a port is in the management whitelist */
static inline bool is_whitelisted_port(u16 port)
{
    int i;
    for (i = 0; i < whitelist_ports_count && i < ARRAY_SIZE(whitelist_ports); i++) {
        if (whitelist_ports[i] && (u16)whitelist_ports[i] == port)
            return true;
    }
    return false;
}

/* Check if a port is a trading port that should be blocked */
static inline bool is_trading_port(u16 port)
{
    return port >= (u16)trading_port_min && port <= (u16)trading_port_max;
}

/* Netfilter hook: only drops packets on trading ports, not management ports */
static unsigned int kill_switch_nf_hook(void *priv, struct sk_buff *skb,
                                        const struct nf_hook_state *state)
{
    struct iphdr *iph;
    u16 dport = 0, sport = 0;

    if (!atomic_read(&kill_switch_active))
        return NF_ACCEPT;

    /* Parse IP header */
    iph = ip_hdr(skb);
    if (!iph)
        return NF_ACCEPT;

    /* Extract port from TCP or UDP */
    if (iph->protocol == IPPROTO_TCP) {
        struct tcphdr *tcph = tcp_hdr(skb);
        if (tcph) {
            dport = ntohs(tcph->dest);
            sport = ntohs(tcph->source);
        }
    } else if (iph->protocol == IPPROTO_UDP) {
        struct udphdr *udph = udp_hdr(skb);
        if (udph) {
            dport = ntohs(udph->dest);
            sport = ntohs(udph->source);
        }
    } else {
        /* Non TCP/UDP (ICMP etc.) — allow through for network diagnostics */
        return NF_ACCEPT;
    }

    /* Always allow whitelisted management ports */
    if (is_whitelisted_port(dport) || is_whitelisted_port(sport))
        return NF_ACCEPT;

    /* Block only trading port range */
    if (is_trading_port(dport) || is_trading_port(sport)) {
        pr_debug("[KILL_SWITCH] Dropping trading packet port %u/%u\n", sport, dport);
        return NF_DROP;
    }

    /* Allow all other traffic */
    return NF_ACCEPT;
}

static int register_netfilter_hook(void)
{
    nf_ops = kmalloc(sizeof(struct nf_hook_ops), GFP_KERNEL);
    if (!nf_ops) return -ENOMEM;
    nf_ops->hook     = kill_switch_nf_hook;
    nf_ops->pf       = NFPROTO_IPV4;
    nf_ops->hooknum  = NF_INET_POST_ROUTING;
    nf_ops->priority = NF_IP_PRI_FIRST;
    {
        int ret = nf_register_net_hook(&init_net, nf_ops);
        if (ret) { kfree(nf_ops); nf_ops = NULL; }
        return ret;
    }
}

static void unregister_netfilter_hook(void)
{
    if (nf_ops) {
        nf_unregister_net_hook(&init_net, nf_ops);
        kfree(nf_ops);
        nf_ops = NULL;
    }
}

static void kill_switch_activate(void)
{
    if (atomic_xchg(&kill_switch_active, 1) == 0) {
        pr_alert("[KILL_SWITCH] EMERGENCY HALT activated on GPIO %d. "
                 "Trading ports %d-%d blocked. Management ports whitelisted.\n",
                 gpio_pin, trading_port_min, trading_port_max);
    }
}

static void kill_switch_deactivate(void)
{
    if (atomic_xchg(&kill_switch_active, 0) == 1) {
        pr_info("[KILL_SWITCH] Kill switch deactivated on GPIO %d\n", gpio_pin);
    }
}

/* Threaded IRQ handler — runs in process context, safe to sleep */
static irqreturn_t gpio_irq_handler_thread(int irq, void *dev_id)
{
    msleep(debounce_ms);
    if (kill_gpio && gpiod_get_value(kill_gpio) == 1) {
        kill_switch_activate();
        return IRQ_HANDLED;
    } else if (kill_gpio && gpiod_get_value(kill_gpio) == 0) {
        /* Rising→falling edge: GPIO went low, deactivate */
        kill_switch_deactivate();
        return IRQ_HANDLED;
    }
    return IRQ_NONE;
}

static int monitor_kill_switch(void *data)
{
    int last_val = 0;
    while (!kthread_should_stop()) {
        if (monitor_gpio) {
            int cur_val = gpiod_get_value(monitor_gpio);
            if (cur_val != last_val) {
                if (cur_val == 1)
                    kill_switch_activate();
                else
                    kill_switch_deactivate();
                last_val = cur_val;
            }
        }
        schedule_timeout_interruptible(HZ / 100); /* Poll at 100 Hz */
    }
    return 0;
}

static int __init kill_switch_init(void)
{
    int ret;

    kill_gpio = gpio_to_desc(gpio_pin);
    if (!kill_gpio) {
        pr_err("[KILL_SWITCH] Invalid GPIO %d\n", gpio_pin);
        return -EINVAL;
    }

    ret = gpiod_direction_input(kill_gpio);
    if (ret) {
        pr_err("[KILL_SWITCH] Failed to set GPIO %d direction: %d\n", gpio_pin, ret);
        return ret;
    }

    irq_number = gpiod_to_irq(kill_gpio);
    if ((int)irq_number < 0) {
        pr_err("[KILL_SWITCH] Failed to get IRQ for GPIO %d: %d\n", gpio_pin, irq_number);
        return (int)irq_number;
    }

    ret = request_threaded_irq(irq_number, NULL, gpio_irq_handler_thread,
                               IRQF_TRIGGER_RISING | IRQF_TRIGGER_FALLING | IRQF_ONESHOT,
                               "kill_switch", NULL);
    if (ret) {
        pr_err("[KILL_SWITCH] IRQ request failed: %d\n", ret);
        return ret;
    }

    ret = register_netfilter_hook();
    if (ret) {
        free_irq(irq_number, NULL);
        return ret;
    }

    monitor_gpio = gpio_to_desc(gpio_pin);
    monitor_thread = kthread_run(monitor_kill_switch, NULL, "kill_switch_monitor");
    if (IS_ERR(monitor_thread)) {
        unregister_netfilter_hook();
        free_irq(irq_number, NULL);
        return PTR_ERR(monitor_thread);
    }

    pr_info("[KILL_SWITCH] v2.1.0 loaded. GPIO %d armed. "
            "Trading ports %d-%d will be blocked on activation. "
            "Whitelisted management ports: 22,8080,8443,9090,...\n",
            gpio_pin, trading_port_min, trading_port_max);
    return 0;
}

static void __exit kill_switch_exit(void)
{
    if (monitor_thread)
        kthread_stop(monitor_thread);
    unregister_netfilter_hook();
    if (irq_number)
        free_irq(irq_number, NULL);
    pr_info("[KILL_SWITCH] Module unloaded\n");
}

module_init(kill_switch_init);
module_exit(kill_switch_exit);
