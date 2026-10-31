#pragma once
#include <QString>

namespace tailui {
class Failsafe {
public:
    bool arm(const QString &library);
    bool disarm();
private:
    QString path_;
};
bool removeAddon(const QString &library, const QString &helpers, QString *error);
} // namespace tailui
