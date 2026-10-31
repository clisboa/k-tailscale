#include <QCoreApplication>
#include <QDir>
#include <QFile>
#include <QTemporaryDir>
#include <cstdio>
#include <cstdlib>
#include "recovery.h"
#include "log.h"

#define CHECK(x) do { if (!(x)) { std::fprintf(stderr,"FAIL line %d: %s\n",__LINE__,#x); std::exit(1); } } while(false)
static void write(const QString &path,const QByteArray &bytes){QFile f(path);CHECK(f.open(QIODevice::WriteOnly));CHECK(f.write(bytes)==bytes.size());}
static QByteArray read(const QString &path){QFile f(path);CHECK(f.open(QIODevice::ReadOnly));return f.readAll();}
int main(int argc,char **argv){
    QCoreApplication app(argc,argv);QTemporaryDir dir;CHECK(dir.isValid());
    const QString library=dir.path()+"/libtailscaleui.so",helpers=dir.path()+"/helpers";
    CHECK(QDir().mkpath(helpers));write(library,"current");
    tailui::Failsafe safe;CHECK(safe.arm(library));CHECK(!QFile::exists(library));CHECK(read(library+".failsafe")=="current");
    CHECK(safe.disarm());CHECK(read(library)=="current");
    write(library,"updated");write(library+".failsafe","old crash");
    tailui::Failsafe next;CHECK(next.arm(library));CHECK(read(library+".failsafe")=="updated");CHECK(next.disarm());
    const QString protectedFile=dir.path()+"/tailscaled.state";write(protectedFile,"keep identity");
    CHECK(QFile::link(protectedFile,helpers+"/control.sh"));write(helpers+"/unrelated","keep helper");
    const QStringList payloads{"migrate.sh","on-boot-root.sh","connect-root.sh","login-root.sh","diagnose-root.sh"};
    for(const QString &name:payloads)write(helpers+"/"+name,"owned payload");
    write(library+".failsafe","old");QString error;
    CHECK(!tailui::removeAddon(protectedFile,helpers,&error));CHECK(read(protectedFile)=="keep identity");
    CHECK(tailui::removeAddon(library,helpers,&error));
    CHECK(!QFile::exists(library)&&!QFile::exists(library+".failsafe")&&!QFile::exists(helpers+"/control.sh"));
    CHECK(read(protectedFile)=="keep identity"&&read(helpers+"/unrelated")=="keep helper");
    for(const QString &name:payloads)CHECK(!QFile::exists(helpers+"/"+name));
    CHECK(tailui::removeAddon(library,helpers,&error));
    const QString redacted=tailui::redact("action failed https://login.tailscale.com/a/abcdef123 tskey-auth-abcdef privkey:abcdef");
    CHECK(redacted.contains("action failed")&&!redacted.contains("abcdef")&&!redacted.contains("https://"));
    std::puts("Failsafe, narrow removal, symlink and credential-redaction tests passed.");
}
