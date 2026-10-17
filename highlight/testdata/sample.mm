#import <Foundation/Foundation.h>

@interface Person : NSObject
@property (nonatomic, strong) NSString *name;
- (instancetype)initWithName:(NSString *)name;
- (void)greet;
@end

@implementation Person
- (instancetype)initWithName:(NSString *)name {
    self = [super init];
    if (self) {
        _name = name;
    }
    return self;
}

- (void)greet {
    NSLog(@"Hello, %@", self.name);
}
@end

int main() {
    @autoreleasepool {
        Person *p = [[Person alloc] initWithName:@"world"];
        [p greet];
    }
    return 0;
}
