#import <Cocoa/Cocoa.h>
#import <ApplicationServices/ApplicationServices.h>
#include <stdlib.h>
#include <string.h>

char *dendrite_sample(int titles, int browser) {
 @autoreleasepool {
  NSRunningApplication *app = [[NSWorkspace sharedWorkspace] frontmostApplication];
  NSString *name = app.localizedName ?: @"";
  NSString *bundle = app.bundleIdentifier ?: name;
  double idle = CGEventSourceSecondsSinceLastEventType(kCGEventSourceStateCombinedSessionState, kCGAnyInputEventType);
  NSDictionary *session = CFBridgingRelease(CGSessionCopyCurrentDictionary());
  BOOL locked = [session[@"CGSSessionScreenIsLocked"] boolValue] || [name isEqualToString:@"loginwindow"];
  NSString *title = @"", *pageURL = @"", *warning = @"";
  BOOL isBrowser = [bundle rangeOfString:@"chrome" options:NSCaseInsensitiveSearch].location != NSNotFound || [bundle rangeOfString:@"safari" options:NSCaseInsensitiveSearch].location != NSNotFound || [bundle rangeOfString:@"firefox" options:NSCaseInsensitiveSearch].location != NSNotFound || [bundle rangeOfString:@"brave" options:NSCaseInsensitiveSearch].location != NSNotFound || [bundle rangeOfString:@"edge" options:NSCaseInsensitiveSearch].location != NSNotFound || [bundle rangeOfString:@"arc" options:NSCaseInsensitiveSearch].location != NSNotFound;
  if ((titles || browser) && !locked) {
   if (!AXIsProcessTrusted()) {
    warning = @"Window titles unavailable. Grant Accessibility access to the Dendrite server (or its launching terminal) in System Settings > Privacy & Security > Accessibility. App and idle tracking still work.";
   } else if (app) {
    AXUIElementRef element = AXUIElementCreateApplication(app.processIdentifier);
    AXUIElementSetMessagingTimeout(element, 0.5);
    CFTypeRef window = NULL;
    if (AXUIElementCopyAttributeValue(element, kAXFocusedWindowAttribute, &window) == kAXErrorSuccess && window) {
     CFTypeRef value = NULL;
     if (AXUIElementCopyAttributeValue((AXUIElementRef)window, kAXTitleAttribute, &value) == kAXErrorSuccess && value) {
      if (CFGetTypeID(value) == CFStringGetTypeID()) title = [(__bridge NSString *)value copy];
      CFRelease(value);
     }
     if (browser && isBrowser) {
      CFTypeRef document = NULL;
      if (AXUIElementCopyAttributeValue((AXUIElementRef)window, kAXDocumentAttribute, &document) == kAXErrorSuccess && document) {
       if (CFGetTypeID(document) == CFStringGetTypeID()) pageURL = [(__bridge NSString *)document copy];
       CFRelease(document);
      }
     }
     CFRelease(window);
    }
    CFRelease(element);
   }
  }
  BOOL privateWindow = [title rangeOfString:@"incognito" options:NSCaseInsensitiveSearch].location != NSNotFound || [title rangeOfString:@"private browsing" options:NSCaseInsensitiveSearch].location != NSNotFound;
  if (!titles) title = @"";
  NSDictionary *payload = @{@"app":name,@"app_id":bundle,@"window_title":title,@"url":pageURL,@"idle_seconds":@(idle),@"locked":@(locked),@"private":@(privateWindow),@"warning":warning};
  NSData *json = [NSJSONSerialization dataWithJSONObject:payload options:0 error:nil];
  return strdup([[NSString alloc] initWithData:json encoding:NSUTF8StringEncoding].UTF8String);
 }
}
