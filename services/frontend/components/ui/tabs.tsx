"use client"

import { Tabs as TabsPrimitive } from "@base-ui/react/tabs"
import { cva, type VariantProps } from "class-variance-authority"
import { cn } from "cn"

function Tabs({ className, orientation = "horizontal", ...props }: TabsPrimitive.Root.Props) {
  return <TabsPrimitive.Root data-slot="tabs" orientation={orientation} className={cn("group/tabs min-w-0", className)} {...props} />
}

const tabsListVariants = cva(
  "group/tabs-list flex min-w-0 items-center text-muted-foreground",
  {
    variants: {
      variant: {
        default: "w-fit gap-1 rounded-lg bg-muted p-1",
        line: "mb-6 w-full gap-6 border-b bg-transparent",
      },
    },
    defaultVariants: { variant: "line" },
  }
)

function TabsList({ className, variant = "line", layout = "scroll", activateOnFocus = false, ...props }: TabsPrimitive.List.Props & VariantProps<typeof tabsListVariants> & { layout?: "scroll" | "responsive" }) {
  return <TabsPrimitive.List
    data-slot="tabs-list"
    data-variant={variant}
    data-layout={layout}
    activateOnFocus={activateOnFocus}
    className={cn(tabsListVariants({ variant }), layout === "responsive" ? "grid grid-cols-2 gap-x-2 gap-y-3 sm:flex sm:gap-6" : "overflow-x-auto", className)}
    {...props}
  />
}

function TabsTrigger({ className, ...props }: TabsPrimitive.Tab.Props) {
  return <TabsPrimitive.Tab
    data-slot="tabs-trigger"
    className={cn(
      "inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap text-sm font-medium text-muted-foreground outline-none transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50",
      "group-data-[variant=line]/tabs-list:rounded-none group-data-[variant=line]/tabs-list:border-b-2 group-data-[variant=line]/tabs-list:border-transparent group-data-[variant=line]/tabs-list:px-1 group-data-[variant=line]/tabs-list:pb-3 group-data-[variant=line]/tabs-list:pt-2 group-data-[variant=line]/tabs-list:data-[active]:border-primary group-data-[variant=line]/tabs-list:data-[active]:text-foreground",
      "group-data-[variant=default]/tabs-list:rounded-md group-data-[variant=default]/tabs-list:px-3 group-data-[variant=default]/tabs-list:py-1 group-data-[variant=default]/tabs-list:data-[active]:bg-background group-data-[variant=default]/tabs-list:data-[active]:text-foreground",
      "group-data-[layout=responsive]/tabs-list:min-h-10 group-data-[layout=responsive]/tabs-list:justify-start group-data-[layout=responsive]/tabs-list:whitespace-normal group-data-[layout=responsive]/tabs-list:text-left group-data-[layout=responsive]/tabs-list:leading-4 sm:group-data-[layout=responsive]/tabs-list:justify-center sm:group-data-[layout=responsive]/tabs-list:whitespace-nowrap",
      className
    )}
    {...props}
  />
}

function TabsContent({ className, ...props }: TabsPrimitive.Panel.Props) {
  return <TabsPrimitive.Panel data-slot="tabs-content" className={cn("min-w-0 outline-none", className)} {...props} />
}

export { Tabs, TabsList, TabsTrigger, TabsContent, tabsListVariants }
