import { Toaster as SonnerToaster } from "sonner";

export function Toaster() {
  return (
    <SonnerToaster
      position="top-center"
      richColors
      closeButton
      containerClassName="fixed top-0 z-50 flex w-full justify-center"
      toastOptions={{
        className:
          "w-full max-w-[420px] rounded-md border bg-background p-4 text-sm text-foreground shadow-md",
        descriptionClassName: "text-muted-foreground",
        actionButtonClassName:
          "bg-primary text-primary-foreground hover:bg-primary/90",
        cancelButtonClassName:
          "bg-muted text-muted-foreground hover:bg-muted/90",
      }}
    />
  );
}

export default Toaster;
