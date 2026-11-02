import java.nio.file.Path;
import javax.xml.XMLConstants;
import javax.xml.parsers.DocumentBuilderFactory;
import javax.xml.transform.TransformerFactory;
import javax.xml.transform.dom.DOMSource;
import javax.xml.transform.stream.StreamResult;

import org.w3c.dom.*;

/**
 * Declares one synthetic body extension so both engines know group boundaries.
 */
public final class DictionaryFixture {
    public static void main(String[] args) throws Exception {
        var factory = DocumentBuilderFactory.newInstance();
        factory.setFeature("http://apache.org/xml/features/disallow-doctype-decl", true);
        Document document = factory.newDocumentBuilder().parse(Path.of("spec/FIX44.xml").toFile());
        NodeList messages = document.getElementsByTagName("message");
        for (int i = 0; i < messages.getLength(); i++) {
            Element message = (Element) messages.item(i);
            String name = message.getAttribute("name");
            if (name.equals("ExecutionReport") || name.equals("NewOrderSingle")) {
                Element field = document.createElement("field");
                field.setAttribute("name", "SyntheticExtension");
                field.setAttribute("required", "N");
                message.appendChild(field);
            }
        }
        Element field = document.createElement("field");
        field.setAttribute("name", "SyntheticExtension");
        field.setAttribute("number", "5001");
        field.setAttribute("type", "STRING");
        document.getElementsByTagName("fields").item(0).appendChild(field);
        var transformer = TransformerFactory.newInstance();
        transformer.setFeature(XMLConstants.FEATURE_SECURE_PROCESSING, true);
        transformer.newTransformer().transform(new DOMSource(document), new StreamResult("/tmp/fix-mux-interop.xml"));
    }
}
