// Nickel image-plugin loading and startup protection adapted from Relio.
// Apache-2.0; see LICENSE and NOTICE.
#include "controller.h"
#include "injector.h"
#include "log.h"
#include "recovery.h"
#include <QApplication>
#include <QFile>
#include <QTimer>
#include <dlfcn.h>
#include <limits.h>
#include <stdlib.h>

namespace {
__attribute__((constructor)) void start(){
    Dl_info info;char path[PATH_MAX]={};
    if(!dladdr(reinterpret_cast<void*>(&start),&info)||!info.dli_fname||!realpath(info.dli_fname,path))return;
    const QString library=QString::fromLocal8Bit(path);
    const QString state=tailui::kStateDir;
    if(QFile::exists(state+"/uninstall")){
        QString error;
        if(tailui::removeAddon(library,"/usr/local/tailscale-ui",&error)){
            QFile::remove(state+"/uninstall");tailui::log("Toolbar addon removed; Tailscale installation retained.");
        }else tailui::log(error);
        return;
    }
    if(!dlopen(path,RTLD_LAZY|RTLD_NODELETE))return;
    static tailui::Failsafe failsafe;
    if(!failsafe.arm(library)){tailui::log("Startup protection could not be armed; toolbar inactive.");return;}
    QApplication *app=qobject_cast<QApplication*>(QCoreApplication::instance());
    if(!app||QFile::exists(state+"/disable-plugin")){failsafe.disarm();return;}
    auto controller=new tailui::Controller("/var/run/tailscale/tailscaled.sock","/usr/local/tailscale-ui/control.sh","/proc/net/route",app);
    controller->prepareRuntime();
    auto injector=new tailui::Injector(controller,app);app->installEventFilter(injector);
    auto scan=new QTimer(app);scan->setSingleShot(true);
    QObject::connect(scan,&QTimer::timeout,injector,[injector]{injector->scanExisting();});scan->start(0);
    auto healthy=new QTimer(app);healthy->setSingleShot(true);
    QObject::connect(healthy,&QTimer::timeout,app,[]{if(!failsafe.disarm())tailui::log("Could not restore plugin after startup.");});healthy->start(20000);
    tailui::log("Tailscale toolbar plugin loaded.");
}
}
