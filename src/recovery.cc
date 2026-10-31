// Startup rename pattern adapted from Relio, Apache-2.0. See NOTICE.
#include "recovery.h"
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QStringList>
#include <cstdio>

namespace tailui {
bool Failsafe::arm(const QString &library) {
    if (QFileInfo(library).fileName() != "libtailscaleui.so") return false;
    path_ = library;
    return ::rename(QFile::encodeName(path_).constData(), QFile::encodeName(path_+".failsafe").constData()) == 0;
}
bool Failsafe::disarm() {
    if (path_.isEmpty() || QFile::exists(path_)) return false;
    return ::rename(QFile::encodeName(path_+".failsafe").constData(), QFile::encodeName(path_).constData()) == 0;
}
bool removeAddon(const QString &library,const QString &helpers,QString *error) {
    if (error) error->clear();
    if (QFileInfo(library).fileName() != "libtailscaleui.so") {
        if(error)*error="Unexpected plugin filename.";
        return false;
    }
    QStringList files{library+".failsafe",library};
    for(const QString &name:QStringList{"control.sh","migrate.sh","on-boot-root.sh","connect-root.sh","login-root.sh","diagnose-root.sh","setup.sh","runtime-boot.sh","release-version"})
        files.prepend(QDir(helpers).filePath(name));
    for (const QString &path:files) {
        const QFileInfo info(path);
        if ((info.exists() || info.isSymLink()) && !QFile::remove(path)) {
            if(error)*error="Could not remove an addon file.";
            return false;
        }
    }
    return true;
}
} // namespace tailui
